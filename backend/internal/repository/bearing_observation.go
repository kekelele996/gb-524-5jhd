package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"spectrum-interference-triangulation/backend/internal/constants"
	"spectrum-interference-triangulation/backend/internal/model"
	"spectrum-interference-triangulation/backend/pkg/api"
)

type ObservationFilter struct {
	CaseID    uint
	StationID uint
	Quality   string
	Page      int
	PageSize  int
}

type ObservationRepository struct {
	db *gorm.DB
}

func NewObservationRepository(db *gorm.DB) *ObservationRepository {
	return &ObservationRepository{db: db}
}

func (r *ObservationRepository) List(ctx context.Context, filter ObservationFilter) ([]model.BearingObservation, int64, error) {
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	query := r.db.WithContext(ctx).Model(&model.BearingObservation{})
	if filter.CaseID > 0 {
		query = query.Where("case_id = ?", filter.CaseID)
	}
	if filter.StationID > 0 {
		query = query.Where("station_id = ?", filter.StationID)
	}
	if filter.Quality != "" {
		query = query.Where("quality = ?", filter.Quality)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count observations: %w", err)
	}
	var observations []model.BearingObservation
	if err := query.Preload("Station").Order("observed_at DESC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&observations).Error; err != nil {
		return nil, 0, fmt.Errorf("list observations: %w", err)
	}
	return observations, total, nil
}

func (r *ObservationRepository) Get(ctx context.Context, id uint) (model.BearingObservation, error) {
	var observation model.BearingObservation
	if err := r.db.WithContext(ctx).Preload("Station").First(&observation, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return model.BearingObservation{}, api.NewError(404, "OBSERVATION_NOT_FOUND", "观测记录不存在")
		}
		return model.BearingObservation{}, fmt.Errorf("get observation: %w", err)
	}
	return observation, nil
}

func (r *ObservationRepository) ListForCase(ctx context.Context, caseID uint, includeExcluded bool) ([]model.BearingObservation, error) {
	query := r.db.WithContext(ctx).Where("case_id = ?", caseID)
	if !includeExcluded {
		query = query.Where("quality <> ?", constants.QualityExcluded)
	}
	var observations []model.BearingObservation
	if err := query.Preload("Station").Order("observed_at ASC").Find(&observations).Error; err != nil {
		return nil, fmt.Errorf("list case observations: %w", err)
	}
	return observations, nil
}

func (r *ObservationRepository) Create(ctx context.Context, observation *model.BearingObservation, actor Actor) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.InterferenceCase{}).Where("id = ? AND case_status <> ?", observation.CaseID, constants.CaseClosed).Count(&count).Error; err != nil {
			return fmt.Errorf("check observation case: %w", err)
		}
		if count == 0 {
			return api.NewError(409, "CASE_READ_ONLY", "案例不存在或已关闭，不能新增观测")
		}
		if err := tx.Create(observation).Error; err != nil {
			return fmt.Errorf("create observation: %w", err)
		}
		audit := NewAudit(actor, "bearing_observation.created", "bearing_observation", observation.ID, nil, observation)
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit observation create: %w", err)
		}
		return nil
	})
}

// BatchImportSummary 是批量导入审计事件中保留的摘要。
type BatchImportSummary struct {
	CaseID        uint                  `json:"case_id"`
	ImportedCount int                   `json:"imported_count"`
	StationCodes  []string              `json:"station_codes"`
	Observations  []BatchImportAuditRow `json:"observations"`
}

type BatchImportAuditRow struct {
	StationID   uint    `json:"station_id"`
	BearingDeg  float64 `json:"bearing_deg"`
	FrequencyHz float64 `json:"frequency_hz"`
	BandwidthHz float64 `json:"bandwidth_hz"`
	SignalDBM   float64 `json:"signal_dbm"`
	Quality     string  `json:"quality"`
	ObservedAt  string  `json:"observed_at"`
}

// CreateBatch 在单个事务内写入整批观测；任一行失败则整批回滚，并留下一条批量导入审计。
func (r *ObservationRepository) CreateBatch(ctx context.Context, observations []model.BearingObservation, summary BatchImportSummary, actor Actor) ([]model.BearingObservation, error) {
	if len(observations) == 0 {
		return nil, api.NewError(400, "EMPTY_BATCH_IMPORT", "批量导入至少需要一行记录")
	}
	caseID := observations[0].CaseID
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.InterferenceCase{}).Where("id = ? AND case_status <> ?", caseID, constants.CaseClosed).Count(&count).Error; err != nil {
			return fmt.Errorf("check observation case: %w", err)
		}
		if count == 0 {
			return api.NewError(409, "CASE_READ_ONLY", "案例不存在或已关闭，不能新增观测")
		}
		if err := tx.CreateInBatches(&observations, len(observations)).Error; err != nil {
			return fmt.Errorf("batch create observations: %w", err)
		}
		ids := make([]uint, 0, len(observations))
		for _, observation := range observations {
			ids = append(ids, observation.ID)
		}
		summary.ImportedCount = len(observations)
		audit := NewAudit(actor, "bearing_observation.batch_imported", "bearing_observation_batch", caseID, nil, map[string]any{
			"summary": summary, "observation_ids": ids,
		})
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit observation batch import: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var created []model.BearingObservation
	if err := r.db.WithContext(ctx).Preload("Station").Where("case_id = ? AND id IN ?", caseID, batchIDs(observations)).Order("observed_at DESC, id DESC").Find(&created).Error; err != nil {
		return nil, fmt.Errorf("reload batch observations: %w", err)
	}
	return created, nil
}

func batchIDs(observations []model.BearingObservation) []uint {
	ids := make([]uint, 0, len(observations))
	for _, observation := range observations {
		ids = append(ids, observation.ID)
	}
	return ids
}

func (r *ObservationRepository) Exclude(ctx context.Context, id uint, reason string, actor Actor) (model.BearingObservation, error) {
	var updated model.BearingObservation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before model.BearingObservation
		if err := tx.First(&before, id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return api.NewError(404, "OBSERVATION_NOT_FOUND", "观测记录不存在")
			}
			return fmt.Errorf("load observation for exclusion: %w", err)
		}
		if before.Quality == constants.QualityExcluded {
			return api.NewError(409, "OBSERVATION_ALREADY_EXCLUDED", "该观测已经被排除")
		}
		var caseRecord model.InterferenceCase
		if err := tx.First(&caseRecord, before.CaseID).Error; err != nil {
			return fmt.Errorf("load observation case: %w", err)
		}
		if caseRecord.CaseStatus == constants.CaseClosed {
			return api.NewError(409, "CASE_READ_ONLY", "案例已关闭，观测只读")
		}
		result := tx.Model(&model.BearingObservation{}).Where("id = ? AND quality <> ?", id, constants.QualityExcluded).Updates(map[string]any{
			"quality": constants.QualityExcluded, "excluded_reason": reason,
		})
		if result.Error != nil {
			return fmt.Errorf("exclude observation: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return api.ErrConflict
		}
		if err := tx.First(&updated, id).Error; err != nil {
			return fmt.Errorf("reload excluded observation: %w", err)
		}
		audit := NewAudit(actor, "bearing_observation.excluded", "bearing_observation", id, before, updated)
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit observation exclusion: %w", err)
		}
		return nil
	})
	return updated, err
}
