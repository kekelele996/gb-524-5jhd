package service

import (
	"context"
	"strconv"
	"strings"

	"spectrum-interference-triangulation/backend/internal/constants"
	"spectrum-interference-triangulation/backend/internal/dto"
	"spectrum-interference-triangulation/backend/internal/model"
	"spectrum-interference-triangulation/backend/internal/repository"
	"spectrum-interference-triangulation/backend/pkg/api"
)

// prepareBatch evaluates the pasted text against the target case. It is shared
// by the dry-run preview and the final commit so the persisted rows always
// match what the analyst confirmed.
func (s *ObservationService) prepareBatch(ctx context.Context, caseID uint, text string) (dto.BatchImportPreviewResponse, []*model.BearingObservation, error) {
	response := dto.BatchImportPreviewResponse{CaseID: caseID, Items: []dto.BatchImportLine{}}
	caseRecord, err := s.caseRepo.Get(ctx, caseID)
	if err != nil {
		return response, nil, err
	}
	if caseRecord.CaseStatus == constants.CaseClosed {
		return response, nil, api.NewError(409, "CASE_READ_ONLY", "案例已关闭，不能录入观测")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return response, nil, api.NewError(400, "BATCH_EMPTY", "请先粘贴至少一行观测记录")
	}

	lineNumbers, lines := parseBatchText(text)
	if len(lines) == 0 {
		return response, nil, api.NewError(400, "BATCH_EMPTY", "粘贴内容中没有可识别的记录行")
	}
	if len(lines) > maxBatchImportRows {
		return response, nil, api.NewError(400, "BATCH_TOO_LARGE", "单次最多导入 200 行记录，请分批粘贴")
	}

	codes := uniqueBatchStationCodes(lines)
	stations, err := s.stationRepo.ListByCodes(ctx, codes)
	if err != nil {
		return response, nil, err
	}
	stationByCode := make(map[string]stationBatchInfo, len(stations))
	for _, station := range stations {
		stationByCode[station.StationCode] = stationBatchInfo{
			id: station.ID, status: station.StationStatus, antennaBiasDeg: station.AntennaBiasDeg,
		}
	}

	observations := make([]*model.BearingObservation, 0, len(lines))
	seen := make(map[string]int, len(lines))
	for index, line := range lines {
		result, observation := validateBatchLine(lineNumbers[index], line, caseID, caseRecord.FrequencyCenterHz, stationByCode)
		if observation != nil {
			key := batchDedupKey(result)
			if first, dup := seen[key]; dup {
				result.valid = false
				result.issues = append(result.issues, "与第 "+strconv.Itoa(first)+" 行内容重复")
				observation = nil
			} else {
				seen[key] = result.lineNumber
			}
		}
		if result.valid {
			response.Valid++
		} else {
			response.Invalid++
		}
		if observation != nil {
			observations = append(observations, observation)
		}
		response.Total++
		response.Items = append(response.Items, dto.BatchImportLine{
			Line: result.lineNumber, StationCode: result.stationCode, StationID: result.stationID,
			StationStatus: result.stationStatus, BearingDeg: result.bearing, SignalDBM: result.signalDBM,
			FrequencyHz: result.frequencyHz, BandwidthHz: result.bandwidthHz, Quality: result.quality,
			ObservedAt: result.observedAtText, FrequencyDeltaHz: result.frequencyDeltaHz,
			Valid: result.valid, Issues: result.issues,
		})
	}
	return response, observations, nil
}

// PreviewBatchImport runs validation only and never writes observations.
func (s *ObservationService) PreviewBatchImport(ctx context.Context, caseID uint, text string) (dto.BatchImportPreviewResponse, error) {
	response, _, err := s.prepareBatch(ctx, caseID, text)
	return response, err
}

// CommitBatchImport re-validates the text and only persists when every row is
// usable. A single invalid row aborts the whole transaction — no partial save.
func (s *ObservationService) CommitBatchImport(ctx context.Context, caseID uint, text string, actor repository.Actor) (dto.BatchImportCommitResponse, error) {
	response, observations, err := s.prepareBatch(ctx, caseID, text)
	if err != nil {
		return dto.BatchImportCommitResponse{}, err
	}
	if response.Invalid > 0 {
		return dto.BatchImportCommitResponse{}, api.WithDetails(api.NewError(422, "BATCH_VALIDATION_FAILED", "存在不可用记录行，整批未保存"), map[string]any{
			"total": response.Total, "valid": response.Valid, "invalid": response.Invalid, "items": response.Items,
		})
	}
	for _, observation := range observations {
		observation.CreatedBy = actor.UserID
	}
	summary, err := s.repo.BatchCreate(ctx, observations, actor)
	if err != nil {
		return dto.BatchImportCommitResponse{}, err
	}
	created := make([]model.BearingObservation, 0, len(observations))
	for _, observation := range observations {
		created = append(created, *observation)
	}
	return dto.BatchImportCommitResponse{
		CaseID: caseID, Imported: len(observations), AuditID: summary.ID, Observations: created,
	}, nil
}

func uniqueBatchStationCodes(lines []parsedBatchLine) []string {
	codes := make([]string, 0, len(lines))
	seen := map[string]struct{}{}
	for _, line := range lines {
		if len(line.fields) != 7 {
			continue
		}
		code := strings.ToUpper(strings.TrimSpace(line.fields[0]))
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	return codes
}

func batchDedupKey(result batchLineResult) string {
	return strings.Join([]string{
		result.stationCode,
		strconv.FormatFloat(result.bearing, 'g', -1, 64),
		strconv.FormatFloat(result.signalDBM, 'g', -1, 64),
		strconv.FormatFloat(result.frequencyHz, 'g', -1, 64),
		strconv.FormatFloat(result.bandwidthHz, 'g', -1, 64),
		result.quality, strings.TrimSpace(result.observedAtText),
	}, "|")
}
