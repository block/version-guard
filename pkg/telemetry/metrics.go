// Package telemetry owns Version Guard application-level Prometheus metrics.
package telemetry

import (
	"errors"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/block/Version-Guard/pkg/types"
)

const (
	ResultSuccess = "success"
	ResultFailure = "failure"

	ScanSourceHTTP   = "http"
	ScanSourceCLI    = "cli"
	ScanSourceManual = "manual"
)

var triggerDurationBuckets = []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

var (
	scanTriggerTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "version_guard_scan_trigger_total",
		Help: "Total Version Guard scan trigger attempts.",
	}, []string{"source", "result", "task_queue"})

	scanTriggerDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "version_guard_scan_trigger_duration_seconds",
		Help:    "Duration of Version Guard scan trigger attempts.",
		Buckets: triggerDurationBuckets,
	}, []string{"source", "result"})

	scanLastTriggerTimestamp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "version_guard_scan_last_trigger_timestamp_seconds",
		Help: "Unix timestamp of the last Version Guard scan trigger attempt.",
	}, []string{"source", "result"})

	detectionResources = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "version_guard_detection_resources",
		Help: "Latest Version Guard detection resource counts by resource type and status.",
	}, []string{"resource_type", "status"})

	detectionComplianceRatio = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "version_guard_detection_compliance_ratio",
		Help: "Latest Version Guard detection compliance ratio by resource type.",
	}, []string{"resource_type"})

	detectionRunTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "version_guard_detection_run_total",
		Help: "Total Version Guard detection workflow results by resource type.",
	}, []string{"resource_type", "result"})

	detectionLastRunTimestamp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "version_guard_detection_last_run_timestamp_seconds",
		Help: "Unix timestamp of the last Version Guard detection workflow result by resource type.",
	}, []string{"resource_type", "result"})

	snapshotCreateAttemptTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "version_guard_snapshot_create_attempt_total",
		Help: "Total Version Guard snapshot creation attempts.",
	}, []string{"result"})
)

// Register adds Version Guard application metrics and process/runtime metrics to
// the same registry used by Temporal SDK metrics.
func Register(registry *prometheus.Registry) error {
	if registry == nil {
		return nil
	}

	collectors := []prometheus.Collector{
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
		scanTriggerTotal,
		scanTriggerDuration,
		scanLastTriggerTimestamp,
		detectionResources,
		detectionComplianceRatio,
		detectionRunTotal,
		detectionLastRunTimestamp,
		snapshotCreateAttemptTotal,
	}

	for _, collector := range collectors {
		if err := registry.Register(collector); err != nil {
			var alreadyRegistered prometheus.AlreadyRegisteredError
			if errors.As(err, &alreadyRegistered) {
				continue
			}
			return err
		}
	}
	return nil
}

// RecordScanTrigger records a manual scan trigger attempt. Correlation IDs stay
// in structured logs, not metric labels.
func RecordScanTrigger(source, result, taskQueue string, duration time.Duration) {
	source = NormalizeScanSource(source)
	if source == ScanSourceCLI {
		return
	}
	result = normalizeResult(result)
	taskQueue = normalizeTaskQueue(taskQueue)

	scanTriggerTotal.WithLabelValues(source, result, taskQueue).Inc()
	scanTriggerDuration.WithLabelValues(source, result).Observe(duration.Seconds())
	scanLastTriggerTimestamp.WithLabelValues(source, result).Set(float64(time.Now().Unix()))
}

// RecordDetectionSummary records the latest per-resource detection summary.
func RecordDetectionSummary(resourceType types.ResourceType, summary *types.ScanSummary) {
	if summary == nil {
		return
	}

	resourceTypeLabel := normalizeLabel(string(resourceType), "unknown")
	detectionResources.WithLabelValues(resourceTypeLabel, "total").Set(float64(summary.TotalResources))
	detectionResources.WithLabelValues(resourceTypeLabel, "red").Set(float64(summary.RedCount))
	detectionResources.WithLabelValues(resourceTypeLabel, "yellow").Set(float64(summary.YellowCount))
	detectionResources.WithLabelValues(resourceTypeLabel, "green").Set(float64(summary.GreenCount))
	detectionResources.WithLabelValues(resourceTypeLabel, "unknown").Set(float64(summary.UnknownCount))

	ratio := 0.0
	if summary.TotalResources > 0 {
		ratio = float64(summary.GreenCount) / float64(summary.TotalResources)
	}
	detectionComplianceRatio.WithLabelValues(resourceTypeLabel).Set(ratio)
}

// RecordDetectionRun records a detection child workflow result.
func RecordDetectionRun(resourceType types.ResourceType, result string) {
	resourceTypeLabel := normalizeLabel(string(resourceType), "unknown")
	result = normalizeResult(result)

	detectionRunTotal.WithLabelValues(resourceTypeLabel, result).Inc()
	detectionLastRunTimestamp.WithLabelValues(resourceTypeLabel, result).Set(float64(time.Now().Unix()))
}

// RecordSnapshotCreateAttempt records a snapshot creation attempt.
func RecordSnapshotCreateAttempt(result string) {
	snapshotCreateAttemptTotal.WithLabelValues(normalizeResult(result)).Inc()
}

// NormalizeScanSource constrains the scan source metric/log label to a small
// enum so new callers cannot create arbitrary Datadog tags.
func NormalizeScanSource(source string) string {
	switch normalizeLabel(source, ScanSourceManual) {
	case ScanSourceHTTP:
		return ScanSourceHTTP
	case ScanSourceCLI:
		return ScanSourceCLI
	case ScanSourceManual:
		return ScanSourceManual
	default:
		return ScanSourceManual
	}
}

func normalizeResult(result string) string {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case ResultSuccess:
		return ResultSuccess
	case ResultFailure:
		return ResultFailure
	default:
		return "unknown"
	}
}

func normalizeLabel(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func normalizeTaskQueue(taskQueue string) string {
	taskQueue = normalizeLabel(taskQueue, "unknown")
	return strings.NewReplacer("-", "_", ".", "_", "/", "_").Replace(taskQueue)
}

// ResetForTest clears package-level metrics between unit tests.
func ResetForTest() {
	scanTriggerTotal.Reset()
	scanTriggerDuration.Reset()
	scanLastTriggerTimestamp.Reset()
	detectionResources.Reset()
	detectionComplianceRatio.Reset()
	detectionRunTotal.Reset()
	detectionLastRunTimestamp.Reset()
	snapshotCreateAttemptTotal.Reset()
}
