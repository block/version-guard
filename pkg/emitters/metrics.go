package emitters

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/DataDog/datadog-go/v5/statsd"

	"github.com/block/Version-Guard/pkg/types"
)

const (
	MetricFindingsTotal        = "version_guard.findings.total"
	MetricFindingsRed          = "version_guard.findings.red"
	MetricFindingsYellow       = "version_guard.findings.yellow"
	MetricFindingsGreen        = "version_guard.findings.green"
	MetricFindingsUnknown      = "version_guard.findings.unknown"
	MetricCompliancePercentage = "version_guard.compliance_percentage"
	MetricDetectionDurationMS  = "version_guard.detection.duration_ms"
	MetricInventoryFetch       = "version_guard.inventory.fetch"
	MetricInventoryResources   = "version_guard.inventory.resources"
	MetricScanCompleted        = "version_guard.scan.completed"
)

// ScanMetrics contains the aggregate scan values emitted to metrics backends.
type ScanMetrics struct {
	Summary        *types.ScanSummary
	DurationMillis int64
}

// InventoryFetchMetrics contains the result of fetching inventory.
type InventoryFetchMetrics struct {
	ResourceType  types.ResourceType
	ResourceCount int
	Success       bool
}

// MetricsEmitter emits aggregate Version Guard scan metrics.
type MetricsEmitter interface {
	EmitScanMetrics(ctx context.Context, metrics ScanMetrics) error
	EmitInventoryFetchMetrics(ctx context.Context, metrics InventoryFetchMetrics) error
}

// NoopMetricsEmitter is the default metrics emitter.
type NoopMetricsEmitter struct{}

// EmitScanMetrics implements MetricsEmitter without emitting anything.
func (NoopMetricsEmitter) EmitScanMetrics(context.Context, ScanMetrics) error {
	return nil
}

// EmitInventoryFetchMetrics implements MetricsEmitter without emitting anything.
func (NoopMetricsEmitter) EmitInventoryFetchMetrics(context.Context, InventoryFetchMetrics) error {
	return nil
}

type statsdClient interface {
	Count(name string, value int64, tags []string, rate float64) error
	Gauge(name string, value float64, tags []string, rate float64) error
	Close() error
}

// DogStatsDMetricsEmitter emits metrics to a DogStatsD-compatible endpoint.
type DogStatsDMetricsEmitter struct {
	client statsdClient
	tags   []string
}

// NewDogStatsDMetricsEmitter creates a DogStatsD-backed metrics emitter.
func NewDogStatsDMetricsEmitter(addr string, tags []string) (*DogStatsDMetricsEmitter, error) {
	if strings.TrimSpace(addr) == "" {
		return nil, fmt.Errorf("dogstatsd address is required")
	}

	client, err := statsd.New(addr)
	if err != nil {
		return nil, fmt.Errorf("create dogstatsd client: %w", err)
	}

	return newDogStatsDMetricsEmitter(client, tags), nil
}

func newDogStatsDMetricsEmitter(client statsdClient, tags []string) *DogStatsDMetricsEmitter {
	return &DogStatsDMetricsEmitter{
		client: client,
		tags:   append([]string(nil), tags...),
	}
}

// Close flushes and closes the underlying DogStatsD client.
func (e *DogStatsDMetricsEmitter) Close() error {
	if e == nil || e.client == nil {
		return nil
	}
	return e.client.Close()
}

// EmitScanMetrics emits scan aggregate metrics as gauges and a completion count.
func (e *DogStatsDMetricsEmitter) EmitScanMetrics(ctx context.Context, metrics ScanMetrics) error {
	if e == nil || e.client == nil || metrics.Summary == nil {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	tags := e.metricTags(metrics.Summary.ResourceType, metrics.Summary.CloudProvider)
	var errs []error

	record := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	record(e.client.Gauge(MetricFindingsTotal, float64(metrics.Summary.TotalResources), tags, 1))
	record(e.client.Gauge(MetricFindingsRed, float64(metrics.Summary.RedCount), tags, 1))
	record(e.client.Gauge(MetricFindingsYellow, float64(metrics.Summary.YellowCount), tags, 1))
	record(e.client.Gauge(MetricFindingsGreen, float64(metrics.Summary.GreenCount), tags, 1))
	record(e.client.Gauge(MetricFindingsUnknown, float64(metrics.Summary.UnknownCount), tags, 1))
	record(e.client.Gauge(MetricCompliancePercentage, metrics.Summary.CompliancePercentage, tags, 1))
	if metrics.DurationMillis > 0 {
		record(e.client.Gauge(MetricDetectionDurationMS, float64(metrics.DurationMillis), tags, 1))
	}
	record(e.client.Count(MetricScanCompleted, 1, tags, 1))

	return errors.Join(errs...)
}

// EmitInventoryFetchMetrics emits inventory fetch success and resource count.
func (e *DogStatsDMetricsEmitter) EmitInventoryFetchMetrics(ctx context.Context, metrics InventoryFetchMetrics) error {
	if e == nil || e.client == nil {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	successValue := 0.0
	if metrics.Success {
		successValue = 1.0
	}

	tags := e.metricTags(metrics.ResourceType, "")
	errs := []error{
		e.client.Gauge(MetricInventoryFetch, successValue, tags, 1),
	}
	if metrics.Success {
		errs = append(errs, e.client.Gauge(MetricInventoryResources, float64(metrics.ResourceCount), tags, 1))
	}

	return errors.Join(errs...)
}

func (e *DogStatsDMetricsEmitter) metricTags(resourceType types.ResourceType, cloudProvider types.CloudProvider) []string {
	tags := append([]string(nil), e.tags...)
	tags = appendMetricTag(tags, "resource_type", strings.ToLower(resourceType.String()))
	tags = appendMetricTag(tags, "cloud_provider", strings.ToLower(cloudProvider.String()))
	return tags
}

func appendMetricTag(tags []string, key, value string) []string {
	if key == "" || value == "" {
		return tags
	}
	return append(tags, key+":"+value)
}
