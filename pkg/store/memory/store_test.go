package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/Version-Guard/pkg/store"
	"github.com/block/Version-Guard/pkg/types"
)

func TestStore_SaveFindings(t *testing.T) {
	ctx := context.Background()
	s := NewStore()

	findings := []*types.Finding{
		{
			ResourceID:   "arn:aws:rds:us-east-1:123:cluster:test-1",
			ResourceType: types.ResourceTypeAurora,
			Status:       types.StatusRed,
			Service:      "payments",
		},
		{
			ResourceID:   "arn:aws:rds:us-east-1:123:cluster:test-2",
			ResourceType: types.ResourceTypeAurora,
			Status:       types.StatusGreen,
			Service:      "billing",
		},
	}

	err := s.SaveFindings(ctx, findings)
	require.NoError(t, err)

	// Verify findings were saved
	assert.Len(t, s.findings, 2)

	// Verify timestamps were set
	for _, f := range findings {
		assert.False(t, f.DetectedAt.IsZero())
		assert.False(t, f.UpdatedAt.IsZero())
	}
}

func TestStore_GetFinding(t *testing.T) {
	ctx := context.Background()
	s := NewStore()

	finding := &types.Finding{
		ResourceID: "arn:aws:rds:us-east-1:123:cluster:test",
		Status:     types.StatusRed,
		Engine:     "aurora-postgresql",
	}

	err := s.SaveFindings(ctx, []*types.Finding{finding})
	require.NoError(t, err)

	// Get existing finding
	result, err := s.GetFinding(ctx, "arn:aws:rds:us-east-1:123:cluster:test")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "aurora-postgresql", result.Engine)
	assert.Equal(t, types.StatusRed, result.Status)

	// Get non-existent finding
	notFound, err := s.GetFinding(ctx, "non-existent")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestStore_ListFindings_NoFilters(t *testing.T) {
	ctx := context.Background()
	s := NewStore()

	findings := []*types.Finding{
		{ResourceID: "1", Status: types.StatusRed},
		{ResourceID: "2", Status: types.StatusGreen},
		{ResourceID: "3", Status: types.StatusYellow},
	}

	err := s.SaveFindings(ctx, findings)
	require.NoError(t, err)

	// List all findings (no filters)
	results, err := s.ListFindings(ctx, store.FindingFilters{})
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestStore_ListFindings_FilterByStatus(t *testing.T) {
	ctx := context.Background()
	s := NewStore()

	findings := []*types.Finding{
		{ResourceID: "1", Status: types.StatusRed, Service: "payments"},
		{ResourceID: "2", Status: types.StatusGreen, Service: "billing"},
		{ResourceID: "3", Status: types.StatusRed, Service: "analytics"},
	}

	err := s.SaveFindings(ctx, findings)
	require.NoError(t, err)

	// Filter by status
	statusRed := types.StatusRed
	results, err := s.ListFindings(ctx, store.FindingFilters{
		Status: &statusRed,
	})
	require.NoError(t, err)
	assert.Len(t, results, 2)

	for _, r := range results {
		assert.Equal(t, types.StatusRed, r.Status)
	}
}

func TestStore_ListFindings_FilterByService(t *testing.T) {
	ctx := context.Background()
	s := NewStore()

	findings := []*types.Finding{
		{ResourceID: "1", Status: types.StatusRed, Service: "payments"},
		{ResourceID: "2", Status: types.StatusGreen, Service: "billing"},
		{ResourceID: "3", Status: types.StatusRed, Service: "payments"},
	}

	err := s.SaveFindings(ctx, findings)
	require.NoError(t, err)

	// Filter by service
	service := "payments"
	results, err := s.ListFindings(ctx, store.FindingFilters{
		Service: &service,
	})
	require.NoError(t, err)
	assert.Len(t, results, 2)

	for _, r := range results {
		assert.Equal(t, "payments", r.Service)
	}
}

func TestStore_ListFindings_MultipleFilters(t *testing.T) {
	ctx := context.Background()
	s := NewStore()

	findings := []*types.Finding{
		{ResourceID: "1", Status: types.StatusRed, Service: "payments", Engine: "aurora-mysql"},
		{ResourceID: "2", Status: types.StatusGreen, Service: "payments", Engine: "aurora-mysql"},
		{ResourceID: "3", Status: types.StatusRed, Service: "billing", Engine: "aurora-postgresql"},
		{ResourceID: "4", Status: types.StatusRed, Service: "payments", Engine: "aurora-postgresql"},
	}

	err := s.SaveFindings(ctx, findings)
	require.NoError(t, err)

	// Filter by status AND service AND engine
	statusRed := types.StatusRed
	service := "payments"
	engine := "aurora-mysql"
	results, err := s.ListFindings(ctx, store.FindingFilters{
		Status:  &statusRed,
		Service: &service,
		Engine:  &engine,
	})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "1", results[0].ResourceID)
}

func TestStore_GetSummary(t *testing.T) {
	ctx := context.Background()
	s := NewStore()

	now := time.Now()
	findings := []*types.Finding{
		{ResourceID: "1", Status: types.StatusRed, DetectedAt: now},
		{ResourceID: "2", Status: types.StatusRed, DetectedAt: now},
		{ResourceID: "3", Status: types.StatusYellow, DetectedAt: now},
		{ResourceID: "4", Status: types.StatusGreen, DetectedAt: now},
		{ResourceID: "5", Status: types.StatusGreen, DetectedAt: now},
		{ResourceID: "6", Status: types.StatusGreen, DetectedAt: now},
	}

	err := s.SaveFindings(ctx, findings)
	require.NoError(t, err)

	summary, err := s.GetSummary(ctx, store.FindingFilters{})
	require.NoError(t, err)
	require.NotNil(t, summary)

	assert.Equal(t, 6, summary.TotalResources)
	assert.Equal(t, 2, summary.RedCount)
	assert.Equal(t, 1, summary.YellowCount)
	assert.Equal(t, 3, summary.GreenCount)
	assert.Equal(t, 0, summary.UnknownCount)

	// Compliance: 3 green / 6 total = 50%
	assert.InDelta(t, 50.0, summary.CompliancePercentage, 0.1)

	// Last scan time should be set
	assert.False(t, summary.LastScanTime.IsZero())
}

func TestStore_ReplaceFindings(t *testing.T) {
	ctx := context.Background()
	s := NewStore()

	err := s.SaveFindings(ctx, []*types.Finding{
		{ResourceID: "1", ResourceType: types.ResourceTypeAurora},
		{ResourceID: "2", ResourceType: types.ResourceTypeAurora},
		{ResourceID: "3", ResourceType: types.ResourceTypeElastiCache},
	})
	require.NoError(t, err)

	// Replace Aurora's set: "2" survives (re-listed), "1" is evicted,
	// "4" is new, and the ElastiCache finding is untouched.
	err = s.ReplaceFindings(ctx, types.ResourceTypeAurora, []*types.Finding{
		{ResourceID: "2", ResourceType: types.ResourceTypeAurora},
		{ResourceID: "4", ResourceType: types.ResourceTypeAurora},
	})
	require.NoError(t, err)

	assert.Len(t, s.findings, 3)
	_, exists := s.findings["1"]
	assert.False(t, exists)
	_, exists = s.findings["2"]
	assert.True(t, exists)
	_, exists = s.findings["4"]
	assert.True(t, exists)
	_, exists = s.findings["3"]
	assert.True(t, exists)

	// UpdatedAt is stamped on the replacement set
	assert.False(t, s.findings["4"].UpdatedAt.IsZero())

	// An empty replacement set empties the type
	err = s.ReplaceFindings(ctx, types.ResourceTypeAurora, nil)
	require.NoError(t, err)
	aurora := types.ResourceTypeAurora
	remaining, err := s.ListFindings(ctx, store.FindingFilters{ResourceType: &aurora})
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

func TestStore_UpdateExistingFinding(t *testing.T) {
	ctx := context.Background()
	s := NewStore()

	// Save initial finding
	finding := &types.Finding{
		ResourceID: "test",
		Status:     types.StatusRed,
	}
	err := s.SaveFindings(ctx, []*types.Finding{finding})
	require.NoError(t, err)

	initialUpdatedAt := finding.UpdatedAt

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Update the same finding
	updatedFinding := &types.Finding{
		ResourceID: "test",
		Status:     types.StatusGreen,
	}
	err = s.SaveFindings(ctx, []*types.Finding{updatedFinding})
	require.NoError(t, err)

	// Retrieve and verify
	result, err := s.GetFinding(ctx, "test")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, types.StatusGreen, result.Status)
	assert.True(t, result.UpdatedAt.After(initialUpdatedAt), "UpdatedAt should be refreshed")
}
