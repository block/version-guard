package snapshot

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/Version-Guard/pkg/types"
)

// TestBuilder_AggregationAcrossGroupings exercises the unified
// StatBucket aggregation: every grouping (resource type, service, cloud
// provider) plus the top-level summary should land identical per-status
// counts, totals, and compliance percentages.
func TestBuilder_AggregationAcrossGroupings(t *testing.T) {
	findings := []*types.Finding{
		{
			ResourceID:    "r1",
			ResourceType:  types.ResourceTypeAurora,
			CloudProvider: types.CloudProviderAWS,
			Service:       "svc-a",
			Status:        types.StatusGreen,
		},
		{
			ResourceID:    "r2",
			ResourceType:  types.ResourceTypeAurora,
			CloudProvider: types.CloudProviderAWS,
			Service:       "svc-a",
			Status:        types.StatusYellow,
		},
		{
			ResourceID:    "r3",
			ResourceType:  types.ResourceTypeAurora,
			CloudProvider: types.CloudProviderAWS,
			Service:       "svc-b",
			Status:        types.StatusRed,
		},
		{
			ResourceID:    "r4",
			ResourceType:  types.ResourceTypeAurora,
			CloudProvider: types.CloudProviderAWS,
			Service:       "", // no service => not counted in ByService
			Status:        types.StatusUnknown,
		},
	}

	snap := NewBuilder().
		WithScanTiming(time.Unix(0, 0), time.Unix(60, 0)).
		AddFindings(types.ResourceTypeAurora, findings).
		Build()

	require.Equal(t, int64(60), snap.ScanDurationSec)

	// Top-level summary
	s := snap.Summary
	assert.Equal(t, 4, s.TotalResources)
	assert.Equal(t, 1, s.GreenCount)
	assert.Equal(t, 1, s.YellowCount)
	assert.Equal(t, 1, s.RedCount)
	assert.Equal(t, 1, s.UnknownCount)
	assert.InDelta(t, 25.0, s.CompliancePercentage, 0.001)

	// ByResourceType
	rt := s.ByResourceType[types.ResourceTypeAurora]
	require.NotNil(t, rt)
	assert.Equal(t, 4, rt.TotalResources)
	assert.Equal(t, 1, rt.GreenCount)
	assert.InDelta(t, 25.0, rt.CompliancePercentage, 0.001)

	// ByService — empty-service finding is excluded.
	require.Len(t, s.ByService, 2)
	assert.Equal(t, 2, s.ByService["svc-a"].TotalResources)
	assert.Equal(t, 1, s.ByService["svc-a"].GreenCount)
	assert.Equal(t, 1, s.ByService["svc-a"].YellowCount)
	assert.InDelta(t, 50.0, s.ByService["svc-a"].CompliancePercentage, 0.001)

	assert.Equal(t, 1, s.ByService["svc-b"].TotalResources)
	assert.Equal(t, 1, s.ByService["svc-b"].RedCount)
	assert.InDelta(t, 0.0, s.ByService["svc-b"].CompliancePercentage, 0.001)

	// ByCloudProvider — every finding contributes.
	require.Len(t, s.ByCloudProvider, 1)
	cp := s.ByCloudProvider[types.CloudProviderAWS]
	assert.Equal(t, 4, cp.TotalResources)
	assert.Equal(t, 1, cp.GreenCount)
	assert.Equal(t, 1, cp.YellowCount)
	assert.Equal(t, 1, cp.RedCount)
	assert.Equal(t, 1, cp.UnknownCount)
	assert.InDelta(t, 25.0, cp.CompliancePercentage, 0.001)
}

// TestBuilder_JSONWireShape locks in the JSON keys produced for the
// snapshot summary so future refactors cannot silently change the wire
// format consumed by downstream tools. Asserts that the dropped
// `by_brand` key is no longer present.
func TestBuilder_JSONWireShape(t *testing.T) {
	snap := NewBuilder().
		AddFindings(types.ResourceTypeAurora, []*types.Finding{
			{
				ResourceID:    "r1",
				ResourceType:  types.ResourceTypeAurora,
				CloudProvider: types.CloudProviderAWS,
				Service:       "svc",
				Status:        types.StatusGreen,
			},
		}).
		Build()

	raw, err := json.Marshal(snap.Summary)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	for _, key := range []string{
		"total_resources", "red_count", "yellow_count", "green_count",
		"unknown_count", "compliance_percentage",
		"by_resource_type", "by_service", "by_cloud_provider",
	} {
		_, ok := decoded[key]
		assert.True(t, ok, "summary missing top-level key %q", key)
	}

	// by_brand was dropped along with brand metadata; make sure it
	// doesn't sneak back in.
	_, hasBrand := decoded["by_brand"]
	assert.False(t, hasBrand, "summary should no longer contain by_brand")

	// Verify a nested bucket also has the canonical keys.
	byService, ok := decoded["by_service"].(map[string]any)
	require.True(t, ok)
	bucket, ok := byService["svc"].(map[string]any)
	require.True(t, ok)
	for _, key := range []string{
		"total_resources", "red_count", "yellow_count", "green_count",
		"unknown_count", "compliance_percentage",
	} {
		_, ok := bucket[key]
		assert.True(t, ok, "bucket missing key %q", key)
	}
}

// TestBuilder_CurrentSchemaBreakWireShape locks in the current schema:
//
//   - snapshot.Version is "v4"
//   - top-level Finding JSON no longer carries ResourceName,
//     CloudAccountID, CloudRegion, or Recommendation
//   - those values flow through Finding.Extra under the YAML logical
//     names "name", "account_id", "region"
//
// Reverting any of these would silently re-introduce the v1 wire shape
// downstream tools have been told to drop, so the test fails fast on
// regressions.
func TestBuilder_CurrentSchemaBreakWireShape(t *testing.T) {
	standardSupportEnd := time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC)
	extendedSupportEnd := time.Date(2027, time.February, 28, 0, 0, 0, 0, time.UTC)
	latestReleaseDate := time.Date(2025, time.February, 13, 0, 0, 0, 0, time.UTC)
	snap := NewBuilder().
		AddFindings(types.ResourceTypeAurora, []*types.Finding{
			{
				ResourceID:    "arn:aws:rds:us-east-1:123:cluster:c1",
				ResourceType:  types.ResourceTypeAurora,
				CloudProvider: types.CloudProviderAWS,
				Service:       "svc",
				Engine:        "aurora-postgresql",
				Status:        types.StatusGreen,
				Extra: map[string]string{
					"name":       "c1",
					"account_id": "123456789012",
					"region":     "us-east-1",
				},
				EOL: types.LifecycleDetails{
					StandardSupportEnd: &standardSupportEnd,
					ExtendedSupportEnd: &extendedSupportEnd,
					EOLDate:            &extendedSupportEnd,
					ActionableDate:     &standardSupportEnd,
					LatestReleaseDate:  &latestReleaseDate,
					Version:            "13",
					Engine:             "aurora-postgresql",
					Source:             "endoflife-date-api",
					IsSupported:        true,
					IsDeprecated:       true,
					IsExtendedSupport:  true,
				},
			},
		}).
		Build()

	assert.Equal(t, "v4", snap.Version,
		"snapshot schema must advertise v3 once recommendation is removed")

	raw, err := json.Marshal(snap)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	findingsByType, ok := decoded["findings_by_type"].(map[string]any)
	require.True(t, ok, "snapshot must carry findings_by_type")

	auroraFindings, ok := findingsByType["AURORA"].([]any)
	require.True(t, ok, "AURORA findings must marshal as a JSON array")
	require.Len(t, auroraFindings, 1)

	finding, ok := auroraFindings[0].(map[string]any)
	require.True(t, ok)

	// Removed top-level keys must stay gone.
	for _, banned := range []string{"ResourceName", "CloudAccountID", "CloudRegion", "Recommendation"} {
		_, present := finding[banned]
		assert.False(t, present,
			"v4 finding JSON must not contain top-level %q", banned)
	}

	// The values now live in Extra under their YAML logical names.
	extra, ok := finding["Extra"].(map[string]any)
	require.True(t, ok, "v4 finding JSON must carry an Extra map")
	assert.Equal(t, "c1", extra["name"])
	assert.Equal(t, "123456789012", extra["account_id"])
	assert.Equal(t, "us-east-1", extra["region"])

	_, hasSimpleEOLDate := finding["EOLDate"]
	assert.False(t, hasSimpleEOLDate, "v4 finding JSON must not expose EOL as a simple date")

	eol, ok := finding["eol"].(map[string]any)
	require.True(t, ok, "v4 finding JSON must carry an eol block for downstream enrichment")
	assert.Equal(t, "2024-02-29T00:00:00Z", eol["standard_support_end"])
	assert.Equal(t, "2027-02-28T00:00:00Z", eol["extended_support_end"])
	assert.Equal(t, "2027-02-28T00:00:00Z", eol["eol_date"])
	assert.Equal(t, "2024-02-29T00:00:00Z", eol["actionable_date"])
	assert.Equal(t, "2025-02-13T00:00:00Z", eol["latest_release_date"])
	assert.Equal(t, "13", eol["version"])
	assert.Equal(t, "aurora-postgresql", eol["engine"])
	assert.Equal(t, "endoflife-date-api", eol["source"])
	assert.Equal(t, true, eol["is_extended_support"])
}
