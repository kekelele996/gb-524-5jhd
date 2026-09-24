package service

import (
	"encoding/csv"
	"math"
	"strconv"
	"strings"
	"time"

	"spectrum-interference-triangulation/backend/internal/constants"
	"spectrum-interference-triangulation/backend/internal/model"
)

const maxBatchImportRows = 200

var batchTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02 15:04",
	"2006/01/02",
}

var qualityLabels = map[string]string{
	"good": "good", "良好": "good", "好": "good", "g": "good",
	"fair": "fair", "一般": "fair", "中": "fair", "f": "fair",
	"poor": "poor", "差": "poor", "偏低": "poor", "p": "poor",
}

// parsedBatchLine holds one parsed row from the pasted multi-station table.
type parsedBatchLine struct {
	raw    string
	fields []string // nil when the line itself cannot be tokenized.
}

type stationBatchInfo struct {
	id             uint
	status         string
	antennaBiasDeg float64
}

type batchLineResult struct {
	lineNumber       int
	stationCode      string
	stationID        uint
	stationStatus    string
	bearing          float64
	signalDBM        float64
	frequencyHz      float64
	bandwidthHz      float64
	quality          string
	observedAtText   string
	frequencyDeltaHz float64
	valid            bool
	issues           []string
}

// splitBatchLine accepts tab-, comma- or semicolon-delimited rows coming from
// spreadsheets or field logs. Tabs (Excel/表格粘贴) take priority.
func splitBatchLine(line string) ([]string, error) {
	delimiter := ','
	switch {
	case strings.ContainsRune(line, '\t'):
		delimiter = '\t'
	case strings.ContainsRune(line, ';'):
		delimiter = ';'
	}
	reader := csv.NewReader(strings.NewReader(line))
	reader.Comma = delimiter
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	return reader.Read()
}

// parseBatchText turns the pasted text into rows, skipping blank lines and an
// optional header row. It returns the physical line numbers alongside the
// parsed rows so error messages can point at the original table location.
func parseBatchText(text string) ([]int, []parsedBatchLine) {
	rawLines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	lineNumbers := make([]int, 0, len(rawLines))
	parsed := make([]parsedBatchLine, 0, len(rawLines))
	for index, raw := range rawLines {
		physical := index + 1
		if strings.TrimSpace(raw) == "" {
			continue
		}
		fields, err := splitBatchLine(raw)
		row := parsedBatchLine{raw: strings.TrimSpace(raw)}
		if err == nil {
			row.fields = fields
		}
		// Skip an optional header row (first non-empty row whose second column
		// is not numeric — data rows always carry a numeric bearing there).
		if len(parsed) == 0 && row.fields != nil && looksLikeBatchHeader(row.fields) {
			continue
		}
		lineNumbers = append(lineNumbers, physical)
		parsed = append(parsed, row)
	}
	return lineNumbers, parsed
}

func looksLikeBatchHeader(fields []string) bool {
	if len(fields) < 2 {
		return false
	}
	_, err := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64)
	return err != nil
}

// validateBatchLine validates one parsed row and returns the row result plus a
// ready-to-persist observation (only populated when the row is fully valid).
func validateBatchLine(lineNumber int, line parsedBatchLine, caseID uint, centerFrequencyHz float64, stationByCode map[string]stationBatchInfo) (batchLineResult, *model.BearingObservation) {
	result := batchLineResult{lineNumber: lineNumber, issues: []string{}}
	if line.fields == nil {
		result.issues = append(result.issues, "行格式无法解析，请使用制表符、逗号或分号分隔的列")
		return result, nil
	}
	if len(line.fields) != 7 {
		result.issues = append(result.issues, "列数应为 7：站点编号、方位、质量、频率、带宽、信号强度、观测时间")
		return result, nil
	}

	result.stationCode = strings.ToUpper(strings.TrimSpace(line.fields[0]))
	parseFloatField(&result, line.fields[1], "方位", &result.bearing)
	result.quality = strings.ToLower(strings.TrimSpace(line.fields[2]))
	parseFloatField(&result, line.fields[3], "频率", &result.frequencyHz)
	parseFloatField(&result, line.fields[4], "带宽", &result.bandwidthHz)
	parseFloatField(&result, line.fields[5], "信号强度", &result.signalDBM)
	result.observedAtText = strings.TrimSpace(line.fields[6])

	if result.stationCode == "" {
		result.issues = append(result.issues, "站点编号不能为空")
	} else if info, found := stationByCode[result.stationCode]; !found {
		result.issues = append(result.issues, "测向站不存在："+result.stationCode)
	} else {
		result.stationID = info.id
		result.stationStatus = info.status
		if info.status != "active" {
			result.issues = append(result.issues, "测向站 "+result.stationCode+" 未处于启用状态")
		}
	}

	if result.bearing < 0 || result.bearing >= 360 {
		result.issues = append(result.issues, "方位必须在 0（含）到 360（不含）度之间")
	}
	if _, ok := qualityLabels[result.quality]; !ok {
		result.issues = append(result.issues, "质量取值无效（good/fair/poor 或 良好/一般/差）")
	}
	if result.frequencyHz <= 0 {
		result.issues = append(result.issues, "频率必须为正数")
	}
	if result.bandwidthHz <= 0 {
		result.issues = append(result.issues, "带宽必须为正数")
	}
	if result.signalDBM < -200 || result.signalDBM > 50 {
		result.issues = append(result.issues, "信号强度必须在 -200 到 50 dBm 之间")
	}

	observedAt := time.Now().UTC()
	if result.observedAtText != "" {
		parsed, ok := parseBatchTime(result.observedAtText)
		if !ok {
			result.issues = append(result.issues, "观测时间格式无效，应为 RFC3339 或 2006-01-02 15:04:05")
		} else {
			observedAt = parsed
		}
	}
	if observedAt.After(time.Now().UTC().Add(5 * time.Minute)) {
		result.issues = append(result.issues, "观测时间不能晚于当前时间")
	}

	if result.frequencyHz > 0 && result.bandwidthHz > 0 {
		result.frequencyDeltaHz = math.Abs(result.frequencyHz - centerFrequencyHz)
		if result.frequencyDeltaHz > result.bandwidthHz/2 {
			result.issues = append(result.issues, "观测频率超出案例中心频率带宽")
		}
	}

	if len(result.issues) > 0 {
		return result, nil
	}

	quality := constants.ObservationQuality(qualityLabels[result.quality])
	info := stationByCode[result.stationCode]
	corrected := normalizeBearing(result.bearing + info.antennaBiasDeg)
	observation := &model.BearingObservation{
		StationID: info.id, CaseID: caseID,
		BearingDeg: result.bearing, CorrectedBearingDeg: corrected,
		SignalDBM: result.signalDBM, FrequencyHz: result.frequencyHz,
		BandwidthHz: result.bandwidthHz, ObservedAt: observedAt,
		Quality: quality,
	}
	result.valid = true
	return result, observation
}

func parseFloatField(result *batchLineResult, raw, label string, target *float64) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		result.issues = append(result.issues, label+"不是有效数字")
		return
	}
	*target = value
}

func parseBatchTime(value string) (time.Time, bool) {
	for _, layout := range batchTimeLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			// Naive timestamps carry no zone; interpret them as UTC.
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}
