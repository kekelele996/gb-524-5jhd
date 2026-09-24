package service

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"spectrum-interference-triangulation/backend/internal/constants"
	"spectrum-interference-triangulation/backend/internal/model"
	"spectrum-interference-triangulation/backend/internal/repository"
)

type batchTestHarness struct {
	*ObservationService
	t      *testing.T
	ctx    context.Context
	db     *gorm.DB
	repo   *repository.ObservationRepository
	actor  repository.Actor
	caseID func(centerHz float64) uint
}

func newBatchTestService(t *testing.T) *batchTestHarness {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.ReceiverStation{}, &model.InterferenceCase{},
		&model.BearingObservation{}, &model.LocalizationEstimate{}, &model.AuditEvent{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC().Add(-72 * time.Hour)
	stations := []model.ReceiverStation{
		{StationCode: "ST01", Name: "一号站", Latitude: 31.2, Longitude: 121.4, AccuracyDeg: 1.2, AntennaBiasDeg: 2, StationStatus: "active", CalibratedAt: &now},
		{StationCode: "ST02", Name: "二号站", Latitude: 31.3, Longitude: 121.5, AccuracyDeg: 1.5, AntennaBiasDeg: -1.5, StationStatus: "active", CalibratedAt: &now},
		{StationCode: "ST03", Name: "三号站", Latitude: 31.4, Longitude: 121.6, AccuracyDeg: 1.8, AntennaBiasDeg: 0, StationStatus: "inactive", CalibratedAt: &now},
	}
	if err := db.Create(&stations).Error; err != nil {
		t.Fatalf("seed stations: %v", err)
	}

	h := &batchTestHarness{
		t:   t,
		ctx: context.Background(),
		db:  db,
		actor: repository.Actor{UserID: 42, Email: "analyst@spectrum.local", Role: constants.RoleAnalyst, RequestID: "test-request"},
	}
	observationRepo := repository.NewObservationRepository(db)
	h.repo = observationRepo
	h.ObservationService = NewObservationService(observationRepo, repository.NewStationRepository(db), repository.NewCaseRepository(db))

	caseSeq := 0
	h.caseID = func(centerHz float64) uint {
		caseSeq++
		record := model.InterferenceCase{
			CaseCode: "RF-T-" + itoaCaseSeq(caseSeq), Title: "测试案例",
			FrequencyCenterHz: centerHz, CaseStatus: constants.CaseCollecting,
			Priority: "normal", OpenedBy: 42, Version: 1,
		}
		if err := db.Create(&record).Error; err != nil {
			t.Fatalf("seed case: %v", err)
		}
		return record.ID
	}
	return h
}

func itoaCaseSeq(value int) string {
	return string(rune('A' + value - 1))
}

func (h *batchTestHarness) countAudits(t *testing.T) int64 {
	var count int64
	if err := h.db.Model(&model.AuditEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count audits: %v", err)
	}
	return count
}

func (h *batchTestHarness) closeCase(id uint) {
	if err := h.db.Model(&model.InterferenceCase{}).Where("id = ?", id).Update("case_status", constants.CaseClosed).Error; err != nil {
		h.t.Fatalf("close case: %v", err)
	}
}
