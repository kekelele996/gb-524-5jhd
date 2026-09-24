package repository

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"spectrum-interference-triangulation/backend/internal/model"
	"spectrum-interference-triangulation/backend/pkg/api"
)

type StationRepository struct {
	db *gorm.DB
}

func NewStationRepository(db *gorm.DB) *StationRepository {
	return &StationRepository{db: db}
}

func (r *StationRepository) List(ctx context.Context, page, pageSize int, status string) ([]model.ReceiverStation, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	query := r.db.WithContext(ctx).Model(&model.ReceiverStation{})
	if status != "" {
		query = query.Where("station_status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count receiver stations: %w", err)
	}
	var stations []model.ReceiverStation
	if err := query.Order("station_code ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&stations).Error; err != nil {
		return nil, 0, fmt.Errorf("list receiver stations: %w", err)
	}
	return stations, total, nil
}

func (r *StationRepository) Get(ctx context.Context, id uint) (model.ReceiverStation, error) {
	var station model.ReceiverStation
	if err := r.db.WithContext(ctx).First(&station, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return model.ReceiverStation{}, api.NewError(404, "STATION_NOT_FOUND", "测向站不存在")
		}
		return model.ReceiverStation{}, fmt.Errorf("get receiver station: %w", err)
	}
	return station, nil
}

func (r *StationRepository) GetByCode(ctx context.Context, code string) (model.ReceiverStation, error) {
	var station model.ReceiverStation
	if err := r.db.WithContext(ctx).Where("UPPER(station_code) = ?", strings.ToUpper(strings.TrimSpace(code))).First(&station).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return model.ReceiverStation{}, api.NewError(404, "STATION_NOT_FOUND", "测向站不存在")
		}
		return model.ReceiverStation{}, fmt.Errorf("get receiver station by code: %w", err)
	}
	return station, nil
}

func (r *StationRepository) ListByCodes(ctx context.Context, codes []string) (map[string]model.ReceiverStation, error) {
	if len(codes) == 0 {
		return map[string]model.ReceiverStation{}, nil
	}
	upperCodes := make([]string, 0, len(codes))
	seen := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		upper := strings.ToUpper(strings.TrimSpace(code))
		if upper == "" {
			continue
		}
		if _, ok := seen[upper]; ok {
			continue
		}
		seen[upper] = struct{}{}
		upperCodes = append(upperCodes, upper)
	}
	var stations []model.ReceiverStation
	if err := r.db.WithContext(ctx).Where("UPPER(station_code) IN ?", upperCodes).Find(&stations).Error; err != nil {
		return nil, fmt.Errorf("list receiver stations by codes: %w", err)
	}
	byCode := make(map[string]model.ReceiverStation, len(stations))
	for _, station := range stations {
		byCode[strings.ToUpper(station.StationCode)] = station
	}
	return byCode, nil
}

func (r *StationRepository) Create(ctx context.Context, station *model.ReceiverStation, actor Actor) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(station).Error; err != nil {
			if err == gorm.ErrDuplicatedKey {
				return api.NewError(409, "STATION_CODE_EXISTS", "测向站编号已存在")
			}
			return fmt.Errorf("create receiver station: %w", err)
		}
		audit := NewAudit(actor, "receiver_station.created", "receiver_station", station.ID, nil, station)
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit receiver station create: %w", err)
		}
		return nil
	})
}

func (r *StationRepository) Update(ctx context.Context, station *model.ReceiverStation, before model.ReceiverStation, actor Actor) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ReceiverStation{}).Where("id = ?", station.ID).Updates(map[string]any{
			"name": station.Name, "latitude": station.Latitude, "longitude": station.Longitude,
			"antenna_bias_deg": station.AntennaBiasDeg, "accuracy_deg": station.AccuracyDeg,
			"station_status": station.StationStatus, "calibrated_at": station.CalibratedAt,
		})
		if result.Error != nil {
			return fmt.Errorf("update receiver station: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return api.NewError(404, "STATION_NOT_FOUND", "测向站不存在")
		}
		audit := NewAudit(actor, "receiver_station.calibrated", "receiver_station", station.ID, before, station)
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit receiver station update: %w", err)
		}
		return nil
	})
}

func (r *StationRepository) Coverage(ctx context.Context, stationID uint) (int64, *model.BearingObservation, error) {
	query := r.db.WithContext(ctx).Model(&model.BearingObservation{}).Where("station_id = ?", stationID)
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, nil, fmt.Errorf("count station observations: %w", err)
	}
	var latest model.BearingObservation
	if err := query.Order("observed_at DESC").First(&latest).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return count, nil, nil
		}
		return 0, nil, fmt.Errorf("latest station observation: %w", err)
	}
	return count, &latest, nil
}
