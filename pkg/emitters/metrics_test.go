package emitters

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/Version-Guard/pkg/types"
)

type metricCall struct {
	name  string
	tags  []string
	value float64
}

type fakeStatsDClient struct {
	err    error
	gauges []metricCall
	counts []metricCall
	closed bool
}

func (f *fakeStatsDClient) Gauge(name string, value float64, tags []string, _ float64) error {
	f.gauges = append(f.gauges, metricCall{name: name, value: value, tags: append([]string(nil), tags...)})
	return f.err
}

func (f *fakeStatsDClient) Count(name string, value int64, tags []string, _ float64) error {
	f.counts = append(f.counts, metricCall{name: name, value: float64(value), tags: append([]string(nil), tags...)})
	return f.err
}

func (f *fakeStatsDClient) Close() error {
	f.closed = true
	return f.err
}

func TestDogStatsDMetricsEmitter_EmitScanMetrics(t *testing.T) {
	client := &fakeStatsDClient{}
	emitter := newDogStatsDMetricsEmitter(client, []string{"service:version-guard", "env:test"})

	err := emitter.EmitScanMetrics(context.Background(), ScanMetrics{
		Summary: &types.ScanSummary{
			TotalResources:       4,
			RedCount:             1,
			YellowCount:          1,
			GreenCount:           1,
			UnknownCount:         1,
			CompliancePercentage: 25,
			ResourceType:         types.ResourceTypeAurora,
			CloudProvider:        types.CloudProviderAWS,
		},
		DurationMillis: 1234,
	})
	require.NoError(t, err)

	assert.Len(t, client.gauges, 7)
	assert.Equal(t, MetricFindingsTotal, client.gauges[0].name)
	assert.Equal(t, 4.0, client.gauges[0].value)
	assert.Equal(t, MetricCompliancePercentage, client.gauges[5].name)
	assert.Equal(t, 25.0, client.gauges[5].value)
	assert.Equal(t, MetricDetectionDurationMS, client.gauges[6].name)
	assert.Equal(t, 1234.0, client.gauges[6].value)
	assert.Equal(t, []string{
		"service:version-guard",
		"env:test",
		"resource_type:aurora",
		"cloud_provider:aws",
	}, client.gauges[0].tags)

	require.Len(t, client.counts, 1)
	assert.Equal(t, MetricScanCompleted, client.counts[0].name)
	assert.Equal(t, 1.0, client.counts[0].value)
}

func TestDogStatsDMetricsEmitter_EmitInventoryFetchMetricsSuccess(t *testing.T) {
	client := &fakeStatsDClient{}
	emitter := newDogStatsDMetricsEmitter(client, []string{"service:version-guard"})

	err := emitter.EmitInventoryFetchMetrics(context.Background(), InventoryFetchMetrics{
		ResourceType:  types.ResourceTypeEKS,
		ResourceCount: 10,
		Success:       true,
	})
	require.NoError(t, err)

	require.Len(t, client.gauges, 2)
	assert.Equal(t, MetricInventoryFetch, client.gauges[0].name)
	assert.Equal(t, 1.0, client.gauges[0].value)
	assert.Equal(t, []string{"service:version-guard", "resource_type:eks"}, client.gauges[0].tags)
	assert.Equal(t, MetricInventoryResources, client.gauges[1].name)
	assert.Equal(t, 10.0, client.gauges[1].value)
}

func TestDogStatsDMetricsEmitter_EmitInventoryFetchMetricsFailure(t *testing.T) {
	client := &fakeStatsDClient{}
	emitter := newDogStatsDMetricsEmitter(client, nil)

	err := emitter.EmitInventoryFetchMetrics(context.Background(), InventoryFetchMetrics{
		ResourceType: types.ResourceTypeEKS,
		Success:      false,
	})
	require.NoError(t, err)

	require.Len(t, client.gauges, 1)
	assert.Equal(t, MetricInventoryFetch, client.gauges[0].name)
	assert.Equal(t, 0.0, client.gauges[0].value)
}

func TestDogStatsDMetricsEmitter_EmitScanMetricsReturnsClientErrors(t *testing.T) {
	clientErr := errors.New("dogstatsd unavailable")
	client := &fakeStatsDClient{err: clientErr}
	emitter := newDogStatsDMetricsEmitter(client, nil)

	err := emitter.EmitScanMetrics(context.Background(), ScanMetrics{
		Summary: &types.ScanSummary{TotalResources: 1},
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, clientErr)
}

func TestDogStatsDMetricsEmitter_Close(t *testing.T) {
	client := &fakeStatsDClient{}
	emitter := newDogStatsDMetricsEmitter(client, nil)

	require.NoError(t, emitter.Close())
	assert.True(t, client.closed)
}
