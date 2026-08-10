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

func TestRecordDetectionBreakdown(t *testing.T) {
	ResetForTest()
	RecordDetectionBreakdown(
		"aurora-mysql",
		map[types.LifecycleUnknownCause]int{types.LifecycleUnknownCauseCycleNotFound: 2},
		map[types.LifecycleDataSource]int{types.LifecycleDataSourceLocalOverride: 3},
	)

	expectedUnknown := `
# HELP version_guard_detection_unknown_resources Latest Version Guard UNKNOWN resource counts by resource type and cause.
# TYPE version_guard_detection_unknown_resources gauge
version_guard_detection_unknown_resources{cause="cycle_not_found",resource_type="aurora-mysql"} 2
version_guard_detection_unknown_resources{cause="empty_inventory_version",resource_type="aurora-mysql"} 0
version_guard_detection_unknown_resources{cause="indeterminate_lifecycle",resource_type="aurora-mysql"} 0
version_guard_detection_unknown_resources{cause="lifecycle_mismatch",resource_type="aurora-mysql"} 0
version_guard_detection_unknown_resources{cause="malformed_cycle",resource_type="aurora-mysql"} 0
version_guard_detection_unknown_resources{cause="product_not_found",resource_type="aurora-mysql"} 0
version_guard_detection_unknown_resources{cause="source_error",resource_type="aurora-mysql"} 0
version_guard_detection_unknown_resources{cause="unattributed",resource_type="aurora-mysql"} 0
`
	expectedSources := `
# HELP version_guard_detection_lifecycle_resources Latest Version Guard detection resource counts by resource type and lifecycle data source.
# TYPE version_guard_detection_lifecycle_resources gauge
version_guard_detection_lifecycle_resources{resource_type="aurora-mysql",source="endoflife_date"} 0
version_guard_detection_lifecycle_resources{resource_type="aurora-mysql",source="local_override"} 3
version_guard_detection_lifecycle_resources{resource_type="aurora-mysql",source="unknown"} 0
`
	require.NoError(t, testutil.CollectAndCompare(detectionUnknownResources, strings.NewReader(expectedUnknown)))
	require.NoError(t, testutil.CollectAndCompare(detectionLifecycleResources, strings.NewReader(expectedSources)))
}

func TestRecordDetectionBreakdownClearsStaleSeries(t *testing.T) {
	ResetForTest()
	RecordDetectionBreakdown("lambda", map[types.LifecycleUnknownCause]int{
		types.LifecycleUnknownCauseSourceError: 4,
	}, nil)
	RecordDetectionBreakdown("lambda", nil, nil)

	require.Equal(t, float64(0), testutil.ToFloat64(
		detectionUnknownResources.WithLabelValues("lambda", "source_error"),
	))
}

func TestRecordDetectionBreakdownNormalizesInvalidValues(t *testing.T) {
	ResetForTest()
	RecordDetectionBreakdown(" ", map[types.LifecycleUnknownCause]int{"": 2, "new-cause": 3},
		map[types.LifecycleDataSource]int{"": 4, "new-source": 5})

	require.Equal(t, float64(5), testutil.ToFloat64(
		detectionUnknownResources.WithLabelValues("unknown", "unattributed"),
	))
	require.Equal(t, float64(9), testutil.ToFloat64(
		detectionLifecycleResources.WithLabelValues("unknown", "unknown"),
	))
	require.Equal(t, len(types.KnownLifecycleUnknownCauses()), testutil.CollectAndCount(detectionUnknownResources))
	require.Equal(t, len(types.KnownLifecycleDataSources()), testutil.CollectAndCount(detectionLifecycleResources))
}

func TestRecordDetectionRun(t *testing.T) {
	ResetForTest()
	RecordDetectionRunWithDuration("eks", ResultFailure, 2*time.Second)

	expected := `
	# HELP version_guard_detection_run_total Total Version Guard detection workflow results by resource type.
	# TYPE version_guard_detection_run_total counter
	version_guard_detection_run_total{resource_type="eks",result="failure"} 1
	`
	require.NoError(t, testutil.CollectAndCompare(detectionRunTotal, strings.NewReader(expected)))
	require.Equal(t, 1, testutil.CollectAndCount(detectionDuration))
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

func TestRecordSnapshotValidation(t *testing.T) {
	ResetForTest()
	RecordSnapshotValidation(ResultFailure, SnapshotValidationReasonMissingResourceType)

	expected := `
	# HELP version_guard_snapshot_validation_total Total Version Guard snapshot validation results.
	# TYPE version_guard_snapshot_validation_total counter
	version_guard_snapshot_validation_total{reason="missing_resource_type",result="failure"} 1
	`
	require.NoError(t, testutil.CollectAndCompare(snapshotValidationTotal, strings.NewReader(expected)))
}

func TestRecordSnapshotResourceTypes(t *testing.T) {
	ResetForTest()
	RecordSnapshotResourceTypes(
		[]types.ResourceType{"aurora-mysql", "lambda"},
		[]types.ResourceType{"aurora-mysql"},
	)

	expectedPresent := `
	# HELP version_guard_snapshot_resource_type_present Whether a resource type is present in the latest full Version Guard snapshot.
	# TYPE version_guard_snapshot_resource_type_present gauge
	version_guard_snapshot_resource_type_present{resource_type="aurora-mysql"} 1
	version_guard_snapshot_resource_type_present{resource_type="lambda"} 0
	`
	require.NoError(t, testutil.CollectAndCompare(snapshotResourceTypePresent, strings.NewReader(expectedPresent)))
	require.Equal(t, 2, testutil.CollectAndCount(snapshotResourceTypeExpected))
}

func TestRecordSnapshotLastValid(t *testing.T) {
	ResetForTest()
	RecordSnapshotLastValid("full")

	require.Equal(t, 1, testutil.CollectAndCount(snapshotLastValidTimestamp))
}
