package dto

import (
	"time"

	"spectrum-interference-triangulation/backend/internal/model"
)

type CreateObservationRequest struct {
	StationID   uint       `json:"station_id" binding:"required"`
	CaseID      uint       `json:"case_id" binding:"required"`
	BearingDeg  float64    `json:"bearing_deg" binding:"gte=0,lt=360"`
	SignalDBM   float64    `json:"signal_dbm" binding:"gte=-200,lte=50"`
	FrequencyHz float64    `json:"frequency_hz" binding:"required,gt=0"`
	BandwidthHz float64    `json:"bandwidth_hz" binding:"required,gt=0"`
	ObservedAt  *time.Time `json:"observed_at"`
	Quality     string     `json:"quality" binding:"required,oneof=good fair poor"`
}

type ExcludeObservationRequest struct {
	Reason string `json:"reason" binding:"required,min=6,max=500"`
}

type ObservationValidation struct {
	ObservationID    uint     `json:"observation_id"`
	Valid            bool     `json:"valid"`
	Issues           []string `json:"issues"`
	FrequencyDeltaHz float64  `json:"frequency_delta_hz"`
}

type BatchValidationResponse struct {
	CaseID  uint                    `json:"case_id"`
	Valid   int                     `json:"valid"`
	Invalid int                     `json:"invalid"`
	Items   []ObservationValidation `json:"items"`
}

// BatchImportRow 是批量导入的一行原始记录，字段顺序与现场多站表格一致：
// 站点编号、方位、频率、带宽、信号、质量、观测时间（可空）。
type BatchImportRow struct {
	StationCode string `json:"station_code"`
	BearingDeg  string `json:"bearing_deg"`
	FrequencyHz string `json:"frequency_hz"`
	BandwidthHz string `json:"bandwidth_hz"`
	SignalDBM   string `json:"signal_dbm"`
	Quality     string `json:"quality"`
	ObservedAt  string `json:"observed_at"`
}

type BatchImportRequest struct {
	CaseID uint             `json:"case_id" binding:"required"`
	Rows   []BatchImportRow `json:"rows" binding:"required,min=1,max=100,dive"`
}

// ImportRowPreview 描述批量预览中单行的可用性、原因以及解析后的标准化值。
type ImportRowPreview struct {
	Line                int      `json:"line"`
	Valid               bool     `json:"valid"`
	Issues              []string `json:"issues"`
	StationCode         string   `json:"station_code"`
	StationID           uint     `json:"station_id"`
	BearingDeg          float64  `json:"bearing_deg"`
	CorrectedBearingDeg float64  `json:"corrected_bearing_deg"`
	FrequencyHz         float64  `json:"frequency_hz"`
	BandwidthHz         float64  `json:"bandwidth_hz"`
	SignalDBM           float64  `json:"signal_dbm"`
	Quality             string   `json:"quality"`
	ObservedAt          string   `json:"observed_at"`
	FrequencyDeltaHz    float64  `json:"frequency_delta_hz"`
}

type BatchImportPreviewResponse struct {
	CaseID            uint               `json:"case_id"`
	CaseCode          string             `json:"case_code"`
	CenterFrequencyHz float64            `json:"center_frequency_hz"`
	Total             int                `json:"total"`
	Valid             int                `json:"valid"`
	Invalid           int                `json:"invalid"`
	Items             []ImportRowPreview `json:"items"`
}

type BatchImportResponse struct {
	CaseID        uint                       `json:"case_id"`
	ImportedCount int                        `json:"imported_count"`
	Observations  []model.BearingObservation `json:"observations"`
}
