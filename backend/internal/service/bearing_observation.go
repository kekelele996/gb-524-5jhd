package service

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"spectrum-interference-triangulation/backend/internal/constants"
	"spectrum-interference-triangulation/backend/internal/dto"
	"spectrum-interference-triangulation/backend/internal/model"
	"spectrum-interference-triangulation/backend/internal/repository"
	"spectrum-interference-triangulation/backend/pkg/api"
)

type ObservationService struct {
	repo        *repository.ObservationRepository
	stationRepo *repository.StationRepository
	caseRepo    *repository.CaseRepository
}

func NewObservationService(repo *repository.ObservationRepository, stationRepo *repository.StationRepository, caseRepo *repository.CaseRepository) *ObservationService {
	return &ObservationService{repo: repo, stationRepo: stationRepo, caseRepo: caseRepo}
}

func (s *ObservationService) List(ctx context.Context, filter repository.ObservationFilter) ([]model.BearingObservation, int64, error) {
	if filter.Quality != "" && !constants.ValidObservationQuality(constants.ObservationQuality(filter.Quality)) {
		return nil, 0, api.NewError(400, "INVALID_OBSERVATION_QUALITY", "观测质量筛选值无效")
	}
	return s.repo.List(ctx, filter)
}

func (s *ObservationService) Get(ctx context.Context, id uint) (model.BearingObservation, error) {
	return s.repo.Get(ctx, id)
}

func (s *ObservationService) Create(ctx context.Context, request dto.CreateObservationRequest, actor repository.Actor) (model.BearingObservation, error) {
	station, err := s.stationRepo.Get(ctx, request.StationID)
	if err != nil {
		return model.BearingObservation{}, err
	}
	if station.StationStatus != "active" {
		return model.BearingObservation{}, api.NewError(409, "STATION_NOT_ACTIVE", "只有已校准且启用的测向站可以录入观测")
	}
	caseRecord, err := s.caseRepo.Get(ctx, request.CaseID)
	if err != nil {
		return model.BearingObservation{}, err
	}
	if caseRecord.CaseStatus == constants.CaseClosed {
		return model.BearingObservation{}, api.NewError(409, "CASE_READ_ONLY", "案例已关闭，不能录入观测")
	}
	if err := validateFrequency(caseRecord.FrequencyCenterHz, request.FrequencyHz, request.BandwidthHz); err != nil {
		return model.BearingObservation{}, err
	}
	observedAt := time.Now().UTC()
	if request.ObservedAt != nil {
		observedAt = request.ObservedAt.UTC()
	}
	if observedAt.After(time.Now().UTC().Add(5 * time.Minute)) {
		return model.BearingObservation{}, api.NewError(422, "INVALID_OBSERVATION_TIME", "观测时间不能晚于当前时间")
	}
	corrected := normalizeBearing(request.BearingDeg + station.AntennaBiasDeg)
	observation := model.BearingObservation{
		StationID: request.StationID, CaseID: request.CaseID,
		BearingDeg: request.BearingDeg, CorrectedBearingDeg: corrected,
		SignalDBM: request.SignalDBM, FrequencyHz: request.FrequencyHz,
		BandwidthHz: request.BandwidthHz, ObservedAt: observedAt,
		Quality: constants.ObservationQuality(request.Quality), CreatedBy: actor.UserID,
	}
	if err := s.repo.Create(ctx, &observation, actor); err != nil {
		return model.BearingObservation{}, err
	}
	observation.Station = &station
	return observation, nil
}

func (s *ObservationService) Exclude(ctx context.Context, id uint, request dto.ExcludeObservationRequest, actor repository.Actor) (model.BearingObservation, error) {
	if actor.Role != constants.RoleAnalyst && actor.Role != constants.RoleAdmin {
		return model.BearingObservation{}, api.ErrForbidden
	}
	return s.repo.Exclude(ctx, id, strings.TrimSpace(request.Reason), actor)
}

func (s *ObservationService) ValidateCase(ctx context.Context, caseID uint) (dto.BatchValidationResponse, error) {
	caseRecord, err := s.caseRepo.Get(ctx, caseID)
	if err != nil {
		return dto.BatchValidationResponse{}, err
	}
	observations, err := s.repo.ListForCase(ctx, caseID, true)
	if err != nil {
		return dto.BatchValidationResponse{}, err
	}
	response := dto.BatchValidationResponse{CaseID: caseID, Items: make([]dto.ObservationValidation, 0, len(observations))}
	for _, observation := range observations {
		item := dto.ObservationValidation{ObservationID: observation.ID, Valid: true, Issues: []string{}}
		item.FrequencyDeltaHz = math.Abs(observation.FrequencyHz - caseRecord.FrequencyCenterHz)
		if observation.Quality == constants.QualityExcluded {
			item.Valid = false
			item.Issues = append(item.Issues, "观测已被人工排除")
		}
		if observation.Station == nil || observation.Station.StationStatus != "active" {
			item.Valid = false
			item.Issues = append(item.Issues, "测向站未处于启用状态")
		}
		if item.FrequencyDeltaHz > observation.BandwidthHz/2 {
			item.Valid = false
			item.Issues = append(item.Issues, "观测频率超出案例中心频率带宽")
		}
		if item.Valid {
			response.Valid++
		} else {
			response.Invalid++
		}
		response.Items = append(response.Items, item)
	}
	return response, nil
}

func validateFrequency(center, observed, bandwidth float64) error {
	delta := math.Abs(center - observed)
	if delta > bandwidth/2 {
		return api.WithDetails(api.NewError(422, "FREQUENCY_MISMATCH", "观测频率超出案例中心频率的有效带宽"), map[string]any{
			"center_hz": center, "observed_hz": observed, "bandwidth_hz": bandwidth, "delta_hz": delta,
		})
	}
	return nil
}

func normalizeBearing(value float64) float64 {
	value = math.Mod(value, 360)
	if value < 0 {
		value += 360
	}
	return value
}

// parsedImportRow 保存单行解析结果，预览和提交共用同一套校验。
type parsedImportRow struct {
	line       int
	issues     []string
	station    *model.ReceiverStation
	bearing    float64
	frequency  float64
	bandwidth  float64
	signalDBM  float64
	quality    constants.ObservationQuality
	observedAt time.Time
}

var qualityAliases = map[string]constants.ObservationQuality{
	"good": constants.QualityGood, "良好": constants.QualityGood, "g": constants.QualityGood,
	"fair": constants.QualityFair, "一般": constants.QualityFair, "f": constants.QualityFair,
	"poor": constants.QualityPoor, "偏低": constants.QualityPoor, "p": constants.QualityPoor,
	"差": constants.QualityPoor,
}

// PreviewBatchImport 逐行校验粘贴的批量记录，返回每行可用性和原因，但不写入任何数据。
func (s *ObservationService) PreviewBatchImport(ctx context.Context, request dto.BatchImportRequest) (dto.BatchImportPreviewResponse, error) {
	caseRecord, stations, err := s.loadBatchContext(ctx, request.CaseID)
	if err != nil {
		return dto.BatchImportPreviewResponse{}, err
	}
	stationByCode := make(map[string]model.ReceiverStation, len(stations))
	for _, station := range stations {
		stationByCode[station.StationCode] = station
	}
	parsed := s.parseImportRows(request.Rows, caseRecord, stationByCode)
	return buildPreview(caseRecord, request.Rows, parsed), nil
}

// BatchImport 在整批记录均可用时一次性写入；任一行不合格则拒绝整批，不保存任何记录。
func (s *ObservationService) BatchImport(ctx context.Context, request dto.BatchImportRequest, actor repository.Actor) (dto.BatchImportResponse, error) {
	if !constants.CanObserve(actor.Role) {
		return dto.BatchImportResponse{}, api.ErrForbidden
	}
	caseRecord, stations, err := s.loadBatchContext(ctx, request.CaseID)
	if err != nil {
		return dto.BatchImportResponse{}, err
	}
	stationByCode := make(map[string]model.ReceiverStation, len(stations))
	for _, station := range stations {
		stationByCode[station.StationCode] = station
	}
	parsed := s.parseImportRows(request.Rows, caseRecord, stationByCode)
	for _, row := range parsed {
		if len(row.issues) > 0 {
			preview := buildPreview(caseRecord, request.Rows, parsed)
			return dto.BatchImportResponse{}, api.WithDetails(api.NewError(422, "BATCH_IMPORT_REJECTED", "存在不合格行，整批记录未保存"), map[string]any{
				"valid": preview.Valid, "invalid": preview.Invalid, "items": preview.Items,
			})
		}
	}
	observations := make([]model.BearingObservation, 0, len(parsed))
	auditRows := make([]repository.BatchImportAuditRow, 0, len(parsed))
	stationCodes := make([]string, 0, len(parsed))
	seenStation := make(map[string]struct{})
	for _, row := range parsed {
		observedAt := row.observedAt
		corrected := normalizeBearing(row.bearing + row.station.AntennaBiasDeg)
		observations = append(observations, model.BearingObservation{
			StationID: row.station.ID, CaseID: caseRecord.ID,
			BearingDeg: row.bearing, CorrectedBearingDeg: corrected,
			SignalDBM: row.signalDBM, FrequencyHz: row.frequency,
			BandwidthHz: row.bandwidth, ObservedAt: observedAt,
			Quality: row.quality, CreatedBy: actor.UserID,
		})
		auditRows = append(auditRows, repository.BatchImportAuditRow{
			StationID: row.station.ID, BearingDeg: row.bearing, FrequencyHz: row.frequency,
			BandwidthHz: row.bandwidth, SignalDBM: row.signalDBM,
			Quality: string(row.quality), ObservedAt: observedAt.Format(time.RFC3339),
		})
		if _, ok := seenStation[row.station.StationCode]; !ok {
			seenStation[row.station.StationCode] = struct{}{}
			stationCodes = append(stationCodes, row.station.StationCode)
		}
	}
	summary := repository.BatchImportSummary{
		CaseID: caseRecord.ID, ImportedCount: len(observations),
		StationCodes: stationCodes, Observations: auditRows,
	}
	created, err := s.repo.CreateBatch(ctx, observations, summary, actor)
	if err != nil {
		return dto.BatchImportResponse{}, err
	}
	return dto.BatchImportResponse{CaseID: caseRecord.ID, ImportedCount: len(created), Observations: created}, nil
}

func (s *ObservationService) loadBatchContext(ctx context.Context, caseID uint) (model.InterferenceCase, []model.ReceiverStation, error) {
	caseRecord, err := s.caseRepo.Get(ctx, caseID)
	if err != nil {
		return model.InterferenceCase{}, nil, err
	}
	if caseRecord.CaseStatus == constants.CaseClosed {
		return model.InterferenceCase{}, nil, api.NewError(409, "CASE_READ_ONLY", "案例已关闭，不能录入观测")
	}
	stations, err := s.stationRepo.ListAll(ctx)
	if err != nil {
		return model.InterferenceCase{}, nil, err
	}
	return caseRecord, stations, nil
}

func (s *ObservationService) parseImportRows(rows []dto.BatchImportRow, caseRecord model.InterferenceCase, stationByCode map[string]model.ReceiverStation) []parsedImportRow {
	parsed := make([]parsedImportRow, 0, len(rows))
	now := time.Now().UTC()
	for index, raw := range rows {
		row := parsedImportRow{line: index + 1, issues: []string{}}

		code := strings.ToUpper(strings.TrimSpace(raw.StationCode))
		if code == "" {
			row.issues = append(row.issues, "测向站编号为空")
		} else if station, ok := stationByCode[code]; ok {
			if station.StationStatus != "active" {
				row.issues = append(row.issues, "测向站 "+code+" 未处于启用状态")
			}
			row.station = &station
		} else {
			row.issues = append(row.issues, "测向站编号 "+code+" 不存在")
		}

		bearing, ok := parseImportNumber(raw.BearingDeg)
		if ok {
			if bearing < 0 || bearing >= 360 {
				row.issues = append(row.issues, "原始方位必须在 [0, 360) 范围内")
			}
			row.bearing = bearing
		} else {
			row.issues = append(row.issues, numericIssue(raw.BearingDeg, "原始方位"))
		}

		frequency, freqOK := parseImportNumber(raw.FrequencyHz)
		if freqOK && frequency > 0 {
			row.frequency = frequency
		} else {
			row.issues = append(row.issues, numericIssue(raw.FrequencyHz, "频率"))
		}

		bandwidth, bwOK := parseImportNumber(raw.BandwidthHz)
		if bwOK && bandwidth > 0 {
			row.bandwidth = bandwidth
		} else {
			row.issues = append(row.issues, numericIssue(raw.BandwidthHz, "带宽"))
		}

		signal, signalOK := parseImportNumber(raw.SignalDBM)
		if signalOK {
			if signal < -200 || signal > 50 {
				row.issues = append(row.issues, "信号强度必须在 -200 到 50 dBm 之间")
			}
			row.signalDBM = signal
		} else {
			row.issues = append(row.issues, numericIssue(raw.SignalDBM, "信号强度"))
		}

		qualityToken := strings.ToLower(strings.TrimSpace(raw.Quality))
		if quality, ok := qualityAliases[qualityToken]; ok {
			row.quality = quality
		} else {
			row.issues = append(row.issues, "质量等级无效（支持 good/fair/poor 或 良好/一般/偏低）")
		}

		observedAt := time.Time{}
		if value := strings.TrimSpace(raw.ObservedAt); value != "" {
			parsedTime, timeErr := parseObservedAt(value)
			if timeErr != nil {
				row.issues = append(row.issues, "观测时间格式无效，支持 RFC3339 或 2006-01-02 15:04:05")
			} else {
				observedAt = parsedTime
			}
		} else {
			observedAt = now
		}
		if !observedAt.IsZero() {
			if observedAt.After(now.Add(5 * time.Minute)) {
				row.issues = append(row.issues, "观测时间不能晚于当前时间")
			}
			row.observedAt = observedAt
		}

		if freqOK && frequency > 0 && bwOK && bandwidth > 0 {
			delta := math.Abs(caseRecord.FrequencyCenterHz - frequency)
			if delta > bandwidth/2 {
				row.issues = append(row.issues, "观测频率超出案例中心频率带宽")
			}
		}

		parsed = append(parsed, row)
	}
	return parsed
}

func buildPreview(caseRecord model.InterferenceCase, raw []dto.BatchImportRow, parsed []parsedImportRow) dto.BatchImportPreviewResponse {
	response := dto.BatchImportPreviewResponse{
		CaseID: caseRecord.ID, CaseCode: caseRecord.CaseCode,
		CenterFrequencyHz: caseRecord.FrequencyCenterHz,
		Total:             len(parsed), Items: make([]dto.ImportRowPreview, 0, len(parsed)),
	}
	for index, row := range parsed {
		item := dto.ImportRowPreview{
			Line: row.line, Valid: len(row.issues) == 0, Issues: row.issues,
			StationCode: strings.ToUpper(strings.TrimSpace(raw[index].StationCode)),
			BearingDeg:  row.bearing, FrequencyHz: row.frequency, BandwidthHz: row.bandwidth,
			SignalDBM: row.signalDBM, Quality: string(row.quality),
		}
		if row.station != nil {
			item.StationID = row.station.ID
			item.CorrectedBearingDeg = normalizeBearing(row.bearing + row.station.AntennaBiasDeg)
		}
		if !row.observedAt.IsZero() {
			item.ObservedAt = row.observedAt.Format(time.RFC3339)
		}
		if row.frequency > 0 {
			item.FrequencyDeltaHz = math.Abs(caseRecord.FrequencyCenterHz - row.frequency)
		}
		if item.Valid {
			response.Valid++
		} else {
			response.Invalid++
		}
		response.Items = append(response.Items, item)
	}
	return response
}

func parseImportNumber(value string) (float64, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}
	number, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

func numericIssue(value, field string) string {
	if strings.TrimSpace(value) == "" {
		return field + "为空"
	}
	return field + "不是有效数字：" + strings.TrimSpace(value)
}

func parseObservedAt(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		time.RFC3339,
	}
	var lastErr error
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}
