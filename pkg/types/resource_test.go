package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourceType_String(t *testing.T) {
	// ResourceType is just a string type; String() is a typed cast.
	// We deliberately test arbitrary values (not just the fixture
	// constants) because production code uses YAML-declared config IDs
	// like "aurora-mysql" — see the ResourceType doc comment.
	tests := []struct {
		rt   ResourceType
		want string
	}{
		{ResourceTypeAurora, "AURORA"},
		{ResourceTypeEKS, "EKS"},
		{ResourceTypeLambda, "LAMBDA"},
		{ResourceType("aurora-mysql"), "aurora-mysql"}, // YAML-declared id
		{ResourceType(""), ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.rt), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.rt.String())
		})
	}
}

// TestStatBucket_JSONShape locks the StatBucket wire keys. Every
// per-grouping bucket (ByResourceType / ByService / ByCloudProvider)
// rolls up through this struct, so changing any key here ripples to
// downstream consumers.
func TestStatBucket_JSONShape(t *testing.T) {
	b := StatBucket{
		TotalResources:       10,
		RedCount:             1,
		YellowCount:          2,
		GreenCount:           6,
		UnknownCount:         1,
		CompliancePercentage: 60.0,
	}
	raw, err := json.Marshal(b)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	for _, key := range []string{
		"total_resources", "red_count", "yellow_count", "green_count",
		"unknown_count", "compliance_percentage",
	} {
		_, ok := decoded[key]
		assert.True(t, ok, "StatBucket missing wire key %q", key)
	}
	assert.Equal(t, float64(10), decoded["total_resources"])
	assert.Equal(t, float64(60), decoded["compliance_percentage"])
}

// TestSnapshot_JSONShape locks the current top-level snapshot wire keys.
// PR #41 stabilized this shape; reordering or renaming any key here is
// a breaking wire-format change and should be intentional.
func TestSnapshot_JSONShape(t *testing.T) {
	s := Snapshot{
		SnapshotID:      "snap-1",
		Version:         "v3",
		ScanDurationSec: 60,
		FindingsByType:  map[ResourceType][]*Finding{},
		Summary:         SnapshotSummary{},
	}
	raw, err := json.Marshal(s)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	for _, key := range []string{
		"snapshot_id", "version", "generated_at",
		"scan_start_time", "scan_end_time", "scan_duration_sec",
		"findings_by_type", "summary",
	} {
		_, ok := decoded[key]
		assert.True(t, ok, "Snapshot missing wire key %q", key)
	}
}
