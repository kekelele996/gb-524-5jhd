package service

import (
	"strings"
	"testing"
)

func testStations() map[string]stationBatchInfo {
	return map[string]stationBatchInfo{
		"ST01": {id: 1, status: "active", antennaBiasDeg: 2},
		"ST02": {id: 2, status: "active", antennaBiasDeg: -1.5},
		"ST03": {id: 3, status: "inactive", antennaBiasDeg: 0},
	}
}

const batchCenterHz = 433_920_000.0

func TestParseBatchTextTabsWithHeader(t *testing.T) {
	text := "站点\t方位\t质量\t频率\t带宽\t信号\t时间\n" +
		"ST01\t45.0\tgood\t433920000\t12500\t-70\t2026-09-24 10:00:00\n" +
		"ST02\t90\tfair\t433921000\t12500\t-72.5\t2026-09-24 10:05:00\n"
	numbers, lines := parseBatchText(text)
	if len(lines) != 2 {
		t.Fatalf("expected 2 data rows, header should be skipped, got %d", len(lines))
	}
	if numbers[0] != 2 || numbers[1] != 3 {
		t.Fatalf("expected physical line numbers [2 3], got %v", numbers)
	}
}

func TestValidateBatchLineSuccessAppliesAntennaBias(t *testing.T) {
	stations := testStations()
	_, lines := parseBatchText("ST01\t45\t良好\t433920000\t12500\t-70\t2026-09-24 10:00:00")
	result, observation := validateBatchLine(1, lines[0], 7, batchCenterHz, stations)
	if !result.valid {
		t.Fatalf("expected valid row, got issues %v", result.issues)
	}
	if observation == nil {
		t.Fatal("expected observation to be built")
	}
	if observation.StationID != 1 || observation.CaseID != 7 {
		t.Fatalf("unexpected station/case ids: %+v", observation)
	}
	if observation.CorrectedBearingDeg != 47 {
		t.Fatalf("expected corrected bearing 47, got %v", observation.CorrectedBearingDeg)
	}
	if string(observation.Quality) != "good" {
		t.Fatalf("expected mapped quality good, got %s", observation.Quality)
	}
}

func TestValidateBatchLineCollectsReasons(t *testing.T) {
	stations := testStations()
	// Unknown station, bad bearing, bad quality, out-of-band frequency, future time.
	_, lines := parseBatchText("NOPE\t400\tweird\t1\t500\t-70\t2099-01-01 00:00:00")
	result, observation := validateBatchLine(4, lines[0], 7, batchCenterHz, stations)
	if result.valid || observation != nil {
		t.Fatal("expected invalid row with no observation")
	}
	if len(result.issues) < 4 {
		t.Fatalf("expected multiple reasons, got %v", result.issues)
	}
}

func TestValidateBatchLineInactiveStation(t *testing.T) {
	stations := testStations()
	_, lines := parseBatchText("ST03\t45\tgood\t433920000\t12500\t-70\t")
	result, _ := validateBatchLine(2, lines[0], 7, batchCenterHz, stations)
	if result.valid {
		t.Fatal("inactive station row must be invalid")
	}
	found := false
	for _, issue := range result.issues {
		if issue == "测向站 ST03 未处于启用状态" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected inactive station reason, got %v", result.issues)
	}
}

func TestValidateBatchLineWrongColumnCount(t *testing.T) {
	_, lines := parseBatchText("ST01,45,good")
	result, observation := validateBatchLine(1, lines[0], 7, batchCenterHz, testStations())
	if result.valid || observation != nil {
		t.Fatal("short row must be invalid")
	}
	if len(result.issues) != 1 {
		t.Fatalf("expected a single column-count reason, got %v", result.issues)
	}
}

func TestParseBatchTimeFormats(t *testing.T) {
	for _, value := range []string{
		"2026-09-24 10:00:00", "2026-09-24T10:00:00Z", "2026/09/24 10:00", "2026-09-24",
	} {
		if _, ok := parseBatchTime(value); !ok {
			t.Errorf("expected %q to parse", value)
		}
	}
	if _, ok := parseBatchTime("not-a-time"); ok {
		t.Error("expected invalid time to fail")
	}
}

func TestPrepareBatchRejectsDuplicateRows(t *testing.T) {
	text := "ST01\t45\tgood\t433920000\t12500\t-70\t2026-09-24 10:00:00\n" +
		"st01\t45\tgood\t433920000\t12500\t-70\t2026-09-24 10:00:00\n"
	service := newBatchTestService(t)
	response, observations, err := service.prepareBatch(service.ctx, service.caseID(433_920_000), text)
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}
	if response.Total != 2 || response.Valid != 1 || response.Invalid != 1 {
		t.Fatalf("expected 1 valid / 1 invalid, got total=%d valid=%d invalid=%d", response.Total, response.Valid, response.Invalid)
	}
	if len(observations) != 1 {
		t.Fatalf("expected only one usable observation, got %d", len(observations))
	}
	if !containsReason(response.Items[1].Issues, "重复") {
		t.Fatalf("expected duplicate reason, got %v", response.Items[1].Issues)
	}
}

func TestPrepareBatchCommitAllOrNothing(t *testing.T) {
	good := "ST01\t45\tgood\t433920000\t12500\t-70\t2026-09-24 10:00:00\n"
	bad := "ST02\t90\tgood\t900000000\t12500\t-70\t2026-09-24 10:00:00\n"
	service := newBatchTestService(t)
	caseID := service.caseID(433_920_000)
	if _, err := service.CommitBatchImport(service.ctx, caseID, good+bad, service.actor); err == nil {
		t.Fatal("expected commit with one invalid row to fail")
	}
	observations, err := service.repo.ListForCase(service.ctx, caseID, true)
	if err != nil {
		t.Fatalf("list observations: %v", err)
	}
	if len(observations) != 0 {
		t.Fatalf("expected zero persisted rows on failed batch, got %d", len(observations))
	}
	audits := service.countAudits(t)
	if audits != 0 {
		t.Fatalf("expected no audit rows on failed batch, got %d", audits)
	}

	response, err := service.CommitBatchImport(service.ctx, caseID, good, service.actor)
	if err != nil {
		t.Fatalf("valid commit failed: %v", err)
	}
	if response.Imported != 1 {
		t.Fatalf("expected 1 imported, got %d", response.Imported)
	}
	if response.AuditID == 0 {
		t.Fatal("expected aggregate audit id")
	}
	observations, _ = service.repo.ListForCase(service.ctx, caseID, true)
	if len(observations) != 1 {
		t.Fatalf("expected 1 persisted row, got %d", len(observations))
	}
	if got := service.countAudits(t); got != 2 {
		t.Fatalf("expected 2 audit rows (created + batch_imported), got %d", got)
	}
}

func TestPrepareBatchRejectsClosedCase(t *testing.T) {
	service := newBatchTestService(t)
	caseID := service.caseID(433_920_000)
	service.closeCase(caseID)
	text := "ST01\t45\tgood\t433920000\t12500\t-70\t2026-09-24 10:00:00\n"
	if _, err := service.PreviewBatchImport(service.ctx, caseID, text); err == nil {
		t.Fatal("expected closed case to be rejected")
	}
}

func containsReason(issues []string, fragment string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, fragment) {
			return true
		}
	}
	return false
}
