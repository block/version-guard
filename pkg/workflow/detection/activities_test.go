package detection

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/block/Version-Guard/pkg/eol"
	eolmock "github.com/block/Version-Guard/pkg/eol/mock"
	"github.com/block/Version-Guard/pkg/inventory"
	invmock "github.com/block/Version-Guard/pkg/inventory/mock"
	"github.com/block/Version-Guard/pkg/policy"
	"github.com/block/Version-Guard/pkg/store/memory"
	"github.com/block/Version-Guard/pkg/types"
)

// newTestActivities creates an Activities instance with mock dependencies.
func newTestActivities(resources []*types.Resource, eolVersions map[string]*types.VersionLifecycle) *Activities {
	mockSource := &invmock.InventorySource{Resources: resources}
	mockEOL := &eolmock.EOLProvider{Versions: eolVersions}
	return NewActivities(
		map[types.ResourceType]inventory.InventorySource{
			types.ResourceTypeAurora:      mockSource,
			types.ResourceTypeElastiCache: mockSource,
		},
		map[types.ResourceType]eol.Provider{
			types.ResourceTypeAurora:      mockEOL,
			types.ResourceTypeElastiCache: mockEOL,
		},
		policy.NewDefaultPolicy(),
		memory.NewStore(),
	)
}

// newActivityEnv creates a TestActivityEnvironment for executing activities.
// Temporal activities call activity.GetLogger(ctx) which requires this env.
func newActivityEnv() *testsuite.TestActivityEnvironment {
	testSuite := &testsuite.WorkflowTestSuite{}
	return testSuite.NewTestActivityEnvironment()
}

func testResources() []*types.Resource {
	return []*types.Resource{
		{
			ID:             "arn:aws:rds:us-east-1:123:cluster:prod-1",
			Type:           types.ResourceTypeAurora,
			CloudProvider:  types.CloudProviderAWS,
			Engine:         "aurora-mysql",
			CurrentVersion: "5.6.10a",
			Extra:          map[string]string{"name": "prod-1"},
		},
		{
			ID:             "arn:aws:rds:us-east-1:123:cluster:prod-2",
			Type:           types.ResourceTypeAurora,
			CloudProvider:  types.CloudProviderAWS,
			Engine:         "aurora-mysql",
			CurrentVersion: "8.0.35",
			Extra:          map[string]string{"name": "prod-2"},
		},
	}
}

func testEOLVersions() map[string]*types.VersionLifecycle {
	return map[string]*types.VersionLifecycle{
		"aurora-mysql:5.6.10a": {Version: "5.6.10a", Engine: "aurora-mysql", IsEOL: true},
		"aurora-mysql:8.0.35":  {Version: "8.0.35", Engine: "aurora-mysql", IsSupported: true},
	}
}

// --- FetchInventory tests ---

func TestFetchInventory_WithScanID_CachesAndReturnsBatchID(t *testing.T) {
	resources := testResources()
	act := newTestActivities(resources, nil)
	env := newActivityEnv()
	env.RegisterActivity(act.FetchInventory)

	result, err := env.ExecuteActivity(act.FetchInventory, FetchInventoryInput{
		ScanID:       "scan-123",
		ResourceType: types.ResourceTypeAurora,
	})
	require.NoError(t, err)

	var inv InventoryResult
	require.NoError(t, result.Get(&inv))

	// Should return batch ID composed of ScanID + ResourceType — the
	// orchestrator fans out one detection workflow per resource type
	// and they all share a ScanID, so the cache key must include the
	// type to avoid parallel workflows clobbering each other.
	expectedBatchID := "scan-123:" + string(types.ResourceTypeAurora)
	assert.Equal(t, expectedBatchID, inv.ResourceBatchID)
	assert.Empty(t, inv.Resources)

	// Verify resources are in cache under the composed key
	cached, ok := act.resourceCache.Load(expectedBatchID)
	require.True(t, ok, "resources should be stored in cache")
	assert.Len(t, cached.([]*types.Resource), 2)
}

func TestFetchInventory_WithoutScanID_ReturnsInline(t *testing.T) {
	resources := testResources()
	act := newTestActivities(resources, nil)
	env := newActivityEnv()
	env.RegisterActivity(act.FetchInventory)

	result, err := env.ExecuteActivity(act.FetchInventory, FetchInventoryInput{
		ResourceType: types.ResourceTypeAurora,
	})
	require.NoError(t, err)

	var inv InventoryResult
	require.NoError(t, result.Get(&inv))

	// Should return inline resources, no batch ID
	assert.Empty(t, inv.ResourceBatchID)
	assert.Len(t, inv.Resources, 2)
}

func TestFetchInventory_SourceError(t *testing.T) {
	errSource := &invmock.InventorySource{ListErr: assert.AnError}
	act := NewActivities(
		map[types.ResourceType]inventory.InventorySource{
			types.ResourceTypeAurora: errSource,
		},
		map[types.ResourceType]eol.Provider{
			types.ResourceTypeAurora: &eolmock.EOLProvider{},
		},
		policy.NewDefaultPolicy(),
		memory.NewStore(),
	)
	env := newActivityEnv()
	env.RegisterActivity(act.FetchInventory)

	_, err := env.ExecuteActivity(act.FetchInventory, FetchInventoryInput{
		ScanID:       "scan-err",
		ResourceType: types.ResourceTypeAurora,
	})
	require.Error(t, err)
}

// --- FetchEOLData tests ---

func TestFetchEOLData_FromCache(t *testing.T) {
	resources := testResources()
	act := newTestActivities(resources, testEOLVersions())
	env := newActivityEnv()
	env.RegisterActivity(act.FetchEOLData)

	// Pre-populate the cache (simulating FetchInventory)
	act.resourceCache.Store("scan-123", resources)

	result, err := env.ExecuteActivity(act.FetchEOLData, FetchEOLInput{
		ResourceType:    types.ResourceTypeAurora,
		ResourceBatchID: "scan-123",
	})
	require.NoError(t, err)

	var eol EOLResult
	require.NoError(t, result.Get(&eol))
	assert.Len(t, eol.VersionLifecycles, 2)
	assert.Contains(t, eol.VersionLifecycles, "aurora-mysql:5.6.10a")
	assert.Contains(t, eol.VersionLifecycles, "aurora-mysql:8.0.35")
}

func TestFetchEOLData_InlineFallback(t *testing.T) {
	resources := testResources()
	act := newTestActivities(resources, testEOLVersions())
	env := newActivityEnv()
	env.RegisterActivity(act.FetchEOLData)

	result, err := env.ExecuteActivity(act.FetchEOLData, FetchEOLInput{
		ResourceType: types.ResourceTypeAurora,
		Resources:    resources,
	})
	require.NoError(t, err)

	var eol EOLResult
	require.NoError(t, result.Get(&eol))
	assert.Len(t, eol.VersionLifecycles, 2)
}

func TestFetchEOLData_CacheMiss(t *testing.T) {
	act := newTestActivities(nil, nil)
	env := newActivityEnv()
	env.RegisterActivity(act.FetchEOLData)

	_, err := env.ExecuteActivity(act.FetchEOLData, FetchEOLInput{
		ResourceBatchID: "nonexistent",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resource batch \"nonexistent\" not found")
}

func TestFetchEOLData_DeduplicatesVersions(t *testing.T) {
	// Two resources with the same engine:version should result in one EOL lookup
	resources := []*types.Resource{
		{ID: "r1", Engine: "aurora-mysql", CurrentVersion: "8.0.35", Type: types.ResourceTypeAurora},
		{ID: "r2", Engine: "aurora-mysql", CurrentVersion: "8.0.35", Type: types.ResourceTypeAurora},
	}
	act := newTestActivities(resources, testEOLVersions())
	env := newActivityEnv()
	env.RegisterActivity(act.FetchEOLData)

	result, err := env.ExecuteActivity(act.FetchEOLData, FetchEOLInput{
		ResourceType: types.ResourceTypeAurora,
		Resources:    resources,
	})
	require.NoError(t, err)

	var eol EOLResult
	require.NoError(t, result.Get(&eol))
	assert.Len(t, eol.VersionLifecycles, 1)
}

// --- DetectDrift tests ---

func TestDetectDrift_FromCache_CleansUpAndStoresFindings(t *testing.T) {
	resources := testResources()
	act := newTestActivities(resources, testEOLVersions())
	env := newActivityEnv()
	env.RegisterActivity(act.DetectDrift)

	// Pre-populate the cache
	act.resourceCache.Store("scan-123", resources)

	result, err := env.ExecuteActivity(act.DetectDrift, DetectInput{
		ResourceBatchID:   "scan-123",
		VersionLifecycles: testEOLVersions(),
	})
	require.NoError(t, err)

	var detect DetectResult
	require.NoError(t, result.Get(&detect))

	assert.Equal(t, 2, detect.FindingsCount)
	assert.Equal(t, "scan-123-findings", detect.FindingsBatchID)
	assert.Empty(t, detect.Findings, "findings should not be inline when using cache")

	// Resource cache should be cleaned up
	_, ok := act.resourceCache.Load("scan-123")
	assert.False(t, ok, "resource cache should be deleted after DetectDrift")

	// Findings should be in cache
	cached, ok := act.resourceCache.Load("scan-123-findings")
	require.True(t, ok, "findings should be stored in cache")
	assert.Len(t, cached.([]*types.Finding), 2)
}

func TestDetectDrift_InlineFallback(t *testing.T) {
	resources := testResources()
	act := newTestActivities(resources, testEOLVersions())
	env := newActivityEnv()
	env.RegisterActivity(act.DetectDrift)

	result, err := env.ExecuteActivity(act.DetectDrift, DetectInput{
		Resources:         resources,
		VersionLifecycles: testEOLVersions(),
	})
	require.NoError(t, err)

	var detect DetectResult
	require.NoError(t, result.Get(&detect))

	assert.Equal(t, 2, detect.FindingsCount)
	assert.Empty(t, detect.FindingsBatchID, "no batch ID when using inline data")
	assert.Len(t, detect.Findings, 2, "findings should be inline")
}

// TestDetectDrift_PropagatesCloudProvider locks in the fix for a bug
// where the workflow path's Finding init silently dropped CloudProvider,
// causing snapshot summaries to aggregate every finding under the
// empty-string CloudProvider key in by_cloud_provider.
func TestDetectDrift_PropagatesCloudProvider(t *testing.T) {
	resources := testResources()
	act := newTestActivities(resources, testEOLVersions())
	env := newActivityEnv()
	env.RegisterActivity(act.DetectDrift)

	result, err := env.ExecuteActivity(act.DetectDrift, DetectInput{
		Resources:         resources,
		VersionLifecycles: testEOLVersions(),
	})
	require.NoError(t, err)

	var detect DetectResult
	require.NoError(t, result.Get(&detect))
	require.Len(t, detect.Findings, 2)

	for _, f := range detect.Findings {
		assert.Equal(t, types.CloudProviderAWS, f.CloudProvider,
			"finding %s lost its CloudProvider", f.ResourceID)
	}
}

// TestDetectDrift_PropagatesExtra ensures any user-defined Extra
// fields on a Resource flow through to the resulting Finding so
// downstream snapshot/JSON consumers see the same map.
func TestDetectDrift_PropagatesExtra(t *testing.T) {
	resources := []*types.Resource{
		{
			ID:             "arn:aws:rds:us-east-1:123:cluster:prod-1",
			Type:           types.ResourceTypeAurora,
			CloudProvider:  types.CloudProviderAWS,
			Engine:         "aurora-mysql",
			CurrentVersion: "5.6.10a",
			Extra: map[string]string{
				"name":        "prod-1",
				"owner":       "team-platform",
				"cost_center": "engineering-prod",
			},
		},
	}
	act := newTestActivities(resources, testEOLVersions())
	env := newActivityEnv()
	env.RegisterActivity(act.DetectDrift)

	result, err := env.ExecuteActivity(act.DetectDrift, DetectInput{
		Resources:         resources,
		VersionLifecycles: testEOLVersions(),
	})
	require.NoError(t, err)

	var detect DetectResult
	require.NoError(t, result.Get(&detect))
	require.Len(t, detect.Findings, 1)

	require.NotNil(t, detect.Findings[0].Extra)
	assert.Equal(t, "team-platform", detect.Findings[0].Extra["owner"])
	assert.Equal(t, "engineering-prod", detect.Findings[0].Extra["cost_center"])
}

func TestDetectDrift_PropagatesLifecycleDetails(t *testing.T) {
	standardSupportEnd := time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC)
	extendedSupportEnd := time.Date(2027, time.February, 28, 0, 0, 0, 0, time.UTC)
	latestReleaseDate := time.Date(2025, time.February, 13, 0, 0, 0, 0, time.UTC)
	fetchedAt := time.Date(2026, time.May, 12, 0, 0, 0, 0, time.UTC)
	resources := []*types.Resource{
		{
			ID:             "arn:aws:rds:us-east-1:123:db:mysql-57",
			Type:           types.ResourceType("rds-mysql"),
			CloudProvider:  types.CloudProviderAWS,
			Engine:         "mysql",
			CurrentVersion: "5.7.44",
		},
	}
	lifecycles := map[string]*types.VersionLifecycle{
		"mysql:5.7.44": {
			Version:            "5.7",
			Engine:             "mysql",
			Source:             "endoflife-date-api",
			DeprecationDate:    &standardSupportEnd,
			ExtendedSupportEnd: &extendedSupportEnd,
			EOLDate:            &extendedSupportEnd,
			LatestReleaseDate:  &latestReleaseDate,
			FetchedAt:          fetchedAt,
			IsSupported:        true,
			IsDeprecated:       true,
			IsExtendedSupport:  true,
		},
	}
	act := newTestActivities(resources, lifecycles)
	env := newActivityEnv()
	env.RegisterActivity(act.DetectDrift)

	result, err := env.ExecuteActivity(act.DetectDrift, DetectInput{
		Resources:         resources,
		VersionLifecycles: lifecycles,
	})
	require.NoError(t, err)

	var detect DetectResult
	require.NoError(t, result.Get(&detect))
	require.Len(t, detect.Findings, 1)

	details := detect.Findings[0].EOL
	assert.Equal(t, "5.7", details.Version)
	assert.Equal(t, "mysql", details.Engine)
	assert.Equal(t, "endoflife-date-api", details.Source)
	require.NotNil(t, details.StandardSupportEnd)
	require.NotNil(t, details.ExtendedSupportEnd)
	require.NotNil(t, details.EOLDate)
	require.NotNil(t, details.LatestReleaseDate)
	require.NotNil(t, details.ActionableDate)
	assert.Equal(t, standardSupportEnd, *details.StandardSupportEnd)
	assert.Equal(t, extendedSupportEnd, *details.ExtendedSupportEnd)
	assert.Equal(t, extendedSupportEnd, *details.EOLDate)
	assert.Equal(t, latestReleaseDate, *details.LatestReleaseDate)
	assert.Equal(t, standardSupportEnd, *details.ActionableDate)
	assert.Equal(t, fetchedAt, details.FetchedAt)
	assert.True(t, details.IsExtendedSupport)
}

func TestDetectDrift_UnknownVersion(t *testing.T) {
	resources := []*types.Resource{
		{ID: "r1", Engine: "aurora-mysql", CurrentVersion: "99.0.0", Type: types.ResourceTypeAurora},
	}
	act := newTestActivities(resources, nil)
	env := newActivityEnv()
	env.RegisterActivity(act.DetectDrift)

	result, err := env.ExecuteActivity(act.DetectDrift, DetectInput{
		Resources:         resources,
		VersionLifecycles: map[string]*types.VersionLifecycle{},
	})
	require.NoError(t, err)

	var detect DetectResult
	require.NoError(t, result.Get(&detect))
	assert.Equal(t, 1, detect.FindingsCount)
}

// --- StoreFindings tests ---

func TestStoreFindings_FromCache(t *testing.T) {
	act := newTestActivities(nil, nil)
	findings := []*types.Finding{
		{ResourceID: "r1", Status: types.StatusRed},
		{ResourceID: "r2", Status: types.StatusGreen},
	}
	act.resourceCache.Store("scan-123-findings", findings)

	env := newActivityEnv()
	env.RegisterActivity(act.StoreFindings)

	_, err := env.ExecuteActivity(act.StoreFindings, StoreInput{
		FindingsBatchID: "scan-123-findings",
	})
	require.NoError(t, err)
}

func TestStoreFindings_InlineFallback(t *testing.T) {
	act := newTestActivities(nil, nil)
	env := newActivityEnv()
	env.RegisterActivity(act.StoreFindings)

	_, err := env.ExecuteActivity(act.StoreFindings, StoreInput{
		Findings: []*types.Finding{
			{ResourceID: "r1", Status: types.StatusGreen},
		},
	})
	require.NoError(t, err)
}

func TestStoreFindings_CacheMiss(t *testing.T) {
	act := newTestActivities(nil, nil)
	env := newActivityEnv()
	env.RegisterActivity(act.StoreFindings)

	_, err := env.ExecuteActivity(act.StoreFindings, StoreInput{
		FindingsBatchID: "nonexistent",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "findings batch \"nonexistent\" not found")
}

// --- EmitMetrics tests ---

func TestEmitMetrics_FromCache_CleansUp(t *testing.T) {
	act := newTestActivities(nil, nil)
	findings := []*types.Finding{
		{ResourceID: "r1", Status: types.StatusRed},
		{ResourceID: "r2", Status: types.StatusGreen},
		{ResourceID: "r3", Status: types.StatusYellow},
		{ResourceID: "r4", Status: types.StatusUnknown},
	}
	act.resourceCache.Store("scan-123-findings", findings)

	env := newActivityEnv()
	env.RegisterActivity(act.EmitMetrics)

	result, err := env.ExecuteActivity(act.EmitMetrics, MetricsInput{
		FindingsBatchID: "scan-123-findings",
		ResourceType:    types.ResourceTypeAurora,
	})
	require.NoError(t, err)

	var metrics MetricsResult
	require.NoError(t, result.Get(&metrics))

	assert.Equal(t, 4, metrics.Summary.TotalResources)
	assert.Equal(t, 1, metrics.Summary.RedCount)
	assert.Equal(t, 1, metrics.Summary.GreenCount)
	assert.Equal(t, 1, metrics.Summary.YellowCount)
	assert.Equal(t, 1, metrics.Summary.UnknownCount)
	assert.Equal(t, 25.0, metrics.Summary.CompliancePercentage)

	// Findings cache should be cleaned up
	_, ok := act.resourceCache.Load("scan-123-findings")
	assert.False(t, ok, "findings cache should be deleted after EmitMetrics")
}

func TestEmitMetrics_InlineFallback(t *testing.T) {
	act := newTestActivities(nil, nil)
	env := newActivityEnv()
	env.RegisterActivity(act.EmitMetrics)

	result, err := env.ExecuteActivity(act.EmitMetrics, MetricsInput{
		Findings: []*types.Finding{
			{ResourceID: "r1", Status: types.StatusGreen},
			{ResourceID: "r2", Status: types.StatusGreen},
		},
		ResourceType: types.ResourceTypeAurora,
	})
	require.NoError(t, err)

	var metrics MetricsResult
	require.NoError(t, result.Get(&metrics))
	assert.Equal(t, 100.0, metrics.Summary.CompliancePercentage)
}

func TestEmitMetrics_EmptyFindings(t *testing.T) {
	act := newTestActivities(nil, nil)
	env := newActivityEnv()
	env.RegisterActivity(act.EmitMetrics)

	result, err := env.ExecuteActivity(act.EmitMetrics, MetricsInput{
		Findings:     []*types.Finding{},
		ResourceType: types.ResourceTypeAurora,
	})
	require.NoError(t, err)

	var metrics MetricsResult
	require.NoError(t, result.Get(&metrics))
	assert.Equal(t, 0, metrics.Summary.TotalResources)
	assert.Equal(t, 0.0, metrics.Summary.CompliancePercentage)
}

// --- Full pipeline cache lifecycle test ---

func TestCacheLifecycle_EndToEnd(t *testing.T) {
	// Simulates the full activity pipeline using caching, verifying that data
	// flows through the cache correctly and is cleaned up at each stage.
	resources := testResources()
	act := newTestActivities(resources, testEOLVersions())

	// Step 1: FetchInventory caches resources
	env1 := newActivityEnv()
	env1.RegisterActivity(act.FetchInventory)
	r1, err := env1.ExecuteActivity(act.FetchInventory, FetchInventoryInput{
		ScanID:       "e2e-scan",
		ResourceType: types.ResourceTypeAurora,
	})
	require.NoError(t, err)
	var inv InventoryResult
	require.NoError(t, r1.Get(&inv))
	expectedBatchID := "e2e-scan:" + string(types.ResourceTypeAurora)
	assert.Equal(t, expectedBatchID, inv.ResourceBatchID)

	// Step 2: FetchEOLData reads from resource cache
	env2 := newActivityEnv()
	env2.RegisterActivity(act.FetchEOLData)
	r2, err := env2.ExecuteActivity(act.FetchEOLData, FetchEOLInput{
		ResourceType:    types.ResourceTypeAurora,
		ResourceBatchID: inv.ResourceBatchID,
	})
	require.NoError(t, err)
	var eol EOLResult
	require.NoError(t, r2.Get(&eol))
	assert.NotEmpty(t, eol.VersionLifecycles)

	// Step 3: DetectDrift reads resources, cleans up, stores findings
	env3 := newActivityEnv()
	env3.RegisterActivity(act.DetectDrift)
	r3, err := env3.ExecuteActivity(act.DetectDrift, DetectInput{
		ResourceBatchID:   inv.ResourceBatchID,
		VersionLifecycles: eol.VersionLifecycles,
	})
	require.NoError(t, err)
	var detect DetectResult
	require.NoError(t, r3.Get(&detect))
	assert.Equal(t, 2, detect.FindingsCount)

	// Resource cache should be gone
	_, ok := act.resourceCache.Load(expectedBatchID)
	assert.False(t, ok, "resource cache should be cleaned up after DetectDrift")

	// Findings cache should exist
	_, ok = act.resourceCache.Load(detect.FindingsBatchID)
	assert.True(t, ok, "findings should be cached")

	// Step 4: StoreFindings reads from findings cache
	env4 := newActivityEnv()
	env4.RegisterActivity(act.StoreFindings)
	_, err = env4.ExecuteActivity(act.StoreFindings, StoreInput{
		FindingsBatchID: detect.FindingsBatchID,
	})
	require.NoError(t, err)

	// Step 5: EmitMetrics reads findings and cleans up
	env5 := newActivityEnv()
	env5.RegisterActivity(act.EmitMetrics)
	r5, err := env5.ExecuteActivity(act.EmitMetrics, MetricsInput{
		FindingsBatchID: detect.FindingsBatchID,
		ResourceType:    types.ResourceTypeAurora,
	})
	require.NoError(t, err)
	var metrics MetricsResult
	require.NoError(t, r5.Get(&metrics))
	assert.Equal(t, 2, metrics.Summary.TotalResources)

	// Findings cache should be gone
	_, ok = act.resourceCache.Load(detect.FindingsBatchID)
	assert.False(t, ok, "findings cache should be cleaned up after EmitMetrics")
}
