package telemetry

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/block/Version-Guard/pkg/types"
)

func TestRegisterExposesApplicationAndRuntimeCollectors(t *testing.T) {
	ResetForTest()
	registry := prometheus.NewRegistry()

	require.NoError(t, Register(registry))
	require.NoError(t, Register(registry))

	names, err := registry.Gather()
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, family := range names {
		seen[family.GetName()] = true
	}

	require.True(t, seen["go_goroutines"])
	require.True(t, seen["process_cpu_seconds_total"])
}

func TestRecordScanTrigger(t *testing.T) {
	ResetForTest()
	RecordScanTrigger("http", ResultSuccess, "version-guard-detection", 25*time.Millisecond)

	expected := `
# HELP version_guard_scan_trigger_total Total Version Guard scan trigger attempts.
# TYPE version_guard_scan_trigger_total counter
version_guard_scan_trigger_total{result="success",source="http",task_queue="version_guard_detection"} 1
`
	require.NoError(t, testutil.CollectAndCompare(scanTriggerTotal, strings.NewReader(expected)))
	require.Equal(t, 1, testutil.CollectAndCount(scanLastTriggerTimestamp))
}

func TestRecordScanTriggerSkipsCLI(t *testing.T) {
	ResetForTest()
	RecordScanTrigger("cli", ResultSuccess, "version-guard-detection", 25*time.Millisecond)

	require.Equal(t, 0, testutil.CollectAndCount(scanTriggerTotal))
	require.Equal(t, 0, testutil.CollectAndCount(scanLastTriggerTimestamp))
}

func TestRecordDetectionSummary(t *testing.T) {
	ResetForTest()
	RecordDetectionSummary("aurora-mysql", &types.ScanSummary{
		TotalResources: 4,
		RedCount:       1,
		YellowCount:    1,
		GreenCount:     2,
	})

	expected := `
# HELP version_guard_detection_compliance_ratio Latest Version Guard detection compliance ratio by resource type.
# TYPE version_guard_detection_compliance_ratio gauge
version_guard_detection_compliance_ratio{resource_type="aurora-mysql"} 0.5
`
	require.NoError(t, testutil.CollectAndCompare(detectionComplianceRatio, strings.NewReader(expected)))
	require.Equal(t, 5, testutil.CollectAndCount(detectionResources))
}

func TestRecordDetectionRun(t *testing.T) {
	ResetForTest()
	RecordDetectionRun("eks", ResultFailure)

	expected := `
# HELP version_guard_detection_run_total Total Version Guard detection workflow results by resource type.
# TYPE version_guard_detection_run_total counter
version_guard_detection_run_total{resource_type="eks",result="failure"} 1
`
	require.NoError(t, testutil.CollectAndCompare(detectionRunTotal, strings.NewReader(expected)))
	require.Equal(t, 1, testutil.CollectAndCount(detectionLastRunTimestamp))
}

func TestRecordSnapshotCreateAttempt(t *testing.T) {
	ResetForTest()
	RecordSnapshotCreateAttempt(ResultFailure)

	expected := `
# HELP version_guard_snapshot_create_attempt_total Total Version Guard snapshot creation attempts.
# TYPE version_guard_snapshot_create_attempt_total counter
version_guard_snapshot_create_attempt_total{result="failure"} 1
`
	require.NoError(t, testutil.CollectAndCompare(snapshotCreateAttemptTotal, strings.NewReader(expected)))
}
