package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"spectrum-interference-triangulation/backend/internal/constants"
	"spectrum-interference-triangulation/backend/internal/dto"
	"spectrum-interference-triangulation/backend/internal/model"
	"spectrum-interference-triangulation/backend/internal/repository"
	"spectrum-interference-triangulation/backend/pkg/api"
)

func newObservationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_foreign_keys=on"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.ReceiverStation{}, &model.InterferenceCase{},
		&model.BearingObservation{}, &model.LocalizationEstimate{}, &model.AuditEvent{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func seedObservationFixture(t *testing.T, db *gorm.DB) (model.InterferenceCase, model.ReceiverStation, model.ReceiverStation) {
	t.Helper()
	calibrated := time.Now().UTC().Add(-48 * time.Hour)
	west := model.ReceiverStation{StationCode: "RX-WEST", Name: "西站", Latitude: 31.2, Longitude: 121.4, AntennaBiasDeg: 0.5, AccuracyDeg: 1.2, StationStatus: "active", CalibratedAt: &calibrated}
	south := model.ReceiverStation{StationCode: "RX-SOUTH", Name: "南站", Latitude: 31.1, Longitude: 121.5, AntennaBiasDeg: -0.5, AccuracyDeg: 1.5, StationStatus: "active", CalibratedAt: &calibrated}
	idle := model.ReceiverStation{StationCode: "RX-IDLE", Name: "停用站", Latitude: 31.0, Longitude: 121.6, AccuracyDeg: 1.5, StationStatus: "inactive"}
	if err := db.Create(&west).Error; err != nil {
		t.Fatalf("create west station: %v", err)
	}
	if err := db.Create(&south).Error; err != nil {
		t.Fatalf("create south station: %v", err)
	}
	if err := db.Create(&idle).Error; err != nil {
		t.Fatalf("create idle station: %v", err)
	}
	caseRecord := model.InterferenceCase{
		CaseCode: "RF-T-001", Title: "批量导入测试案例", FrequencyCenterHz: 433_920_000,
		CaseStatus: constants.CaseCollecting, Priority: "normal", OpenedBy: 1, Version: 1,
	}
	if err := db.Create(&caseRecord).Error; err != nil {
		t.Fatalf("create case: %v", err)
	}
	return caseRecord, west, south
}

func newObservationService(db *gorm.DB) *ObservationService {
	return NewObservationService(
		repository.NewObservationRepository(db),
		repository.NewStationRepository(db),
		repository.NewCaseRepository(db),
	)
}

func validRows() []dto.BatchImportRow {
	return []dto.BatchImportRow{
		{StationCode: "RX-WEST", BearingDeg: "90", FrequencyHz: "433920000", BandwidthHz: "12500", SignalDBM: "-70", Quality: "good"},
		{StationCode: "rx-south", BearingDeg: "100.5", FrequencyHz: "433921000", BandwidthHz: "12500", SignalDBM: "-75", Quality: "一般"},
	}
}

func TestPreviewBatchImportFlagsEachRow(t *testing.T) {
	db := newObservationTestDB(t)
	caseRecord, _, _ := seedObservationFixture(t, db)
	service := newObservationService(db)

	rows := append(validRows(),
		dto.BatchImportRow{StationCode: "RX-NOPE", BearingDeg: "50", FrequencyHz: "433920000", BandwidthHz: "12500", SignalDBM: "-70", Quality: "good"},
		dto.BatchImportRow{StationCode: "RX-IDLE", BearingDeg: "50", FrequencyHz: "433920000", BandwidthHz: "12500", SignalDBM: "-70", Quality: "good"},
		dto.BatchImportRow{StationCode: "RX-WEST", BearingDeg: "400", FrequencyHz: "433920000", BandwidthHz: "12500", SignalDBM: "-70", Quality: "good"},
		dto.BatchImportRow{StationCode: "RX-WEST", BearingDeg: "50", FrequencyHz: "434000000", BandwidthHz: "12500", SignalDBM: "-70", Quality: "good"},
		dto.BatchImportRow{StationCode: "RX-WEST", BearingDeg: "abc", FrequencyHz: "433920000", BandwidthHz: "12500", SignalDBM: "-70", Quality: "good"},
	)
	preview, err := service.PreviewBatchImport(context.Background(), dto.BatchImportRequest{CaseID: caseRecord.ID, Rows: rows})
	if err != nil {
		t.Fatalf("preview batch: %v", err)
	}
	if preview.Total != 7 || preview.Valid != 2 || preview.Invalid != 5 {
		t.Fatalf("unexpected preview counts: total=%d valid=%d invalid=%d", preview.Total, preview.Valid, preview.Invalid)
	}
	if preview.Items[0].StationID == 0 || preview.Items[0].CorrectedBearingDeg != 90.5 {
		t.Fatalf("valid row should resolve station and corrected bearing: %+v", preview.Items[0])
	}
	for _, item := range preview.Items[2:] {
		if item.Valid || len(item.Issues) == 0 {
			t.Fatalf("line %d expected invalid with issues, got %+v", item.Line, item)
		}
	}
}

func TestBatchImportRejectsWholeBatchWhenAnyRowInvalid(t *testing.T) {
	db := newObservationTestDB(t)
	caseRecord, _, _ := seedObservationFixture(t, db)
	service := newObservationService(db)
	actor := repository.Actor{UserID: 1, Email: "observer@spectrum.local", Role: constants.RoleObserver, RequestID: "req-reject"}

	rows := append(validRows(), dto.BatchImportRow{
		StationCode: "RX-WEST", BearingDeg: "50", FrequencyHz: "900000000", BandwidthHz: "12500", SignalDBM: "-70", Quality: "good",
	})
	_, err := service.BatchImport(context.Background(), dto.BatchImportRequest{CaseID: caseRecord.ID, Rows: rows}, actor)
	var appErr *api.Error
	if err == nil {
		t.Fatal("expected batch import to be rejected")
	}
	if !errors.As(err, &appErr) || appErr.Code != "BATCH_IMPORT_REJECTED" {
		t.Fatalf("expected BATCH_IMPORT_REJECTED, got %v", err)
	}
	var observationCount int64
	if err := db.Model(&model.BearingObservation{}).Count(&observationCount).Error; err != nil {
		t.Fatalf("count observations: %v", err)
	}
	if observationCount != 0 {
		t.Fatalf("invalid batch must save nothing, found %d observations", observationCount)
	}
	var auditCount int64
	if err := db.Model(&model.AuditEvent{}).Where("action = ?", "bearing_observation.batch_imported").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if auditCount != 0 {
		t.Fatalf("rejected batch must not leave an import audit, found %d", auditCount)
	}
}

func TestBatchImportPersistsAllRowsAndAudit(t *testing.T) {
	db := newObservationTestDB(t)
	caseRecord, west, south := seedObservationFixture(t, db)
	service := newObservationService(db)
	actor := repository.Actor{UserID: 1, Email: "observer@spectrum.local", Role: constants.RoleObserver, RequestID: "req-import"}

	result, err := service.BatchImport(context.Background(), dto.BatchImportRequest{CaseID: caseRecord.ID, Rows: validRows()}, actor)
	if err != nil {
		t.Fatalf("batch import: %v", err)
	}
	if result.ImportedCount != 2 || len(result.Observations) != 2 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	byStation := map[uint]model.BearingObservation{}
	for _, observation := range result.Observations {
		if observation.Station == nil {
			t.Fatalf("imported observation should preload station: %+v", observation)
		}
		byStation[observation.StationID] = observation
	}
	if got := byStation[west.ID].CorrectedBearingDeg; got != 90.5 {
		t.Fatalf("west corrected bearing = %v, want 90.5", got)
	}
	if got := byStation[south.ID].CorrectedBearingDeg; got != 100.0 {
		t.Fatalf("south corrected bearing = %v, want 100.0", got)
	}

	stored, err := service.repo.ListForCase(context.Background(), caseRecord.ID, false)
	if err != nil {
		t.Fatalf("list case observations: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("expected 2 stored observations, got %d", len(stored))
	}

	var auditEvent model.AuditEvent
	if err := db.Where("action = ?", "bearing_observation.batch_imported").First(&auditEvent).Error; err != nil {
		t.Fatalf("expected batch import audit event: %v", err)
	}
	if auditEvent.EntityType != "bearing_observation_batch" || auditEvent.EntityID != caseRecord.ID {
		t.Fatalf("unexpected audit target: %+v", auditEvent)
	}
	if auditEvent.RequestID != "req-import" {
		t.Fatalf("audit missing request id: %+v", auditEvent)
	}
}

func TestBatchImportForbiddenForReviewer(t *testing.T) {
	db := newObservationTestDB(t)
	caseRecord, _, _ := seedObservationFixture(t, db)
	service := newObservationService(db)
	actor := repository.Actor{UserID: 2, Email: "reviewer@spectrum.local", Role: constants.RoleReviewer, RequestID: "req-deny"}

	_, err := service.BatchImport(context.Background(), dto.BatchImportRequest{CaseID: caseRecord.ID, Rows: validRows()}, actor)
	var appErr *api.Error
	if !errors.As(err, &appErr) || appErr.Code != "ACCESS_DENIED" {
		t.Fatalf("expected ACCESS_DENIED, got %v", err)
	}
}
