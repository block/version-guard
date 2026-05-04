package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/block/Version-Guard/pkg/snapshot"
	"github.com/block/Version-Guard/pkg/store/memory"
	"github.com/block/Version-Guard/pkg/types"
)

// fakeSnapshotStore captures the snapshot the activity tries to
// persist so the test can assert on it without needing a real S3
// bucket or the s3API fake.
type fakeSnapshotStore struct {
	saved         *types.Snapshot
	saveError     error
	saveCallCount int
}

func (f *fakeSnapshotStore) SaveSnapshot(_ context.Context, s *types.Snapshot) error {
	f.saveCallCount++
	if f.saveError != nil {
		return f.saveError
	}
	f.saved = s
	return nil
}

func (f *fakeSnapshotStore) GetLatestSnapshot(_ context.Context) (*types.Snapshot, error) {
	return f.saved, nil
}

func (f *fakeSnapshotStore) GetSnapshot(_ context.Context, _ string) (*types.Snapshot, error) {
	return f.saved, nil
}

func (f *fakeSnapshotStore) ListSnapshots(_ context.Context, _ int) ([]*snapshot.Metadata, error) {
	return nil, nil
}

// runCreateSnapshotActivity executes the activity through Temporal's
// activity test environment so the activity.GetLogger / activity
// context plumbing works correctly. Takes the input by pointer to keep
// gocritic/hugeParam quiet — the activity itself takes it by value
// (Temporal SDK convention).
func runCreateSnapshotActivity(t *testing.T, a *Activities, in *CreateSnapshotInput) (*SnapshotResult, error) {
	t.Helper()
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivity(a.CreateSnapshot)

	val, err := env.ExecuteActivity(a.CreateSnapshot, *in)
	if err != nil {
		return nil, err
	}
	var result SnapshotResult
	require.NoError(t, val.Get(&result))
	return &result, nil
}

func TestActivities_CreateSnapshot_HappyPath(t *testing.T) {
	st := memory.NewStore()
	fakeSnap := &fakeSnapshotStore{}
	a := NewActivities(st, fakeSnap)

	require.NotNil(t, a)
	require.Same(t, st, a.Store)
	require.Same(t, fakeSnap, a.SnapshotStore)

	// Seed the in-memory store with findings under two resource types.
	require.NoError(t, st.SaveFindings(context.Background(), []*types.Finding{
		{
			ResourceID:    "r-aurora-1",
			ResourceType:  types.ResourceTypeAurora,
			CloudProvider: types.CloudProviderAWS,
			Service:       "svc-a",
			Status:        types.StatusGreen,
		},
		{
			ResourceID:    "r-aurora-2",
			ResourceType:  types.ResourceTypeAurora,
			CloudProvider: types.CloudProviderAWS,
			Service:       "svc-a",
			Status:        types.StatusRed,
		},
		{
			ResourceID:    "r-eks-1",
			ResourceType:  types.ResourceTypeEKS,
			CloudProvider: types.CloudProviderAWS,
			Service:       "svc-b",
			Status:        types.StatusGreen,
		},
	}))

	start := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	end := start.Add(60 * time.Second)

	result, err := runCreateSnapshotActivity(t, a, &CreateSnapshotInput{
		ScanID:        "scan-123",
		ResourceTypes: []types.ResourceType{types.ResourceTypeAurora, types.ResourceTypeEKS},
		ScanStartTime: start,
		ScanEndTime:   end,
	})
	require.NoError(t, err)

	assert.Equal(t, "scan-123", result.SnapshotID, "SnapshotID must be the scan-id passed in for correlation")
	assert.Equal(t, 3, result.TotalFindings)
	assert.InDelta(t, 66.67, result.CompliancePercentage, 0.1)

	// And the snapshot was persisted exactly once.
	require.Equal(t, 1, fakeSnap.saveCallCount)
	require.NotNil(t, fakeSnap.saved)
	assert.Equal(t, "scan-123", fakeSnap.saved.SnapshotID)
	assert.Equal(t, "v3", fakeSnap.saved.Version)
	assert.Equal(t, int64(60), fakeSnap.saved.ScanDurationSec)
	assert.Equal(t, 3, fakeSnap.saved.Summary.TotalResources)
}

func TestActivities_CreateSnapshot_PersistFailureReturnsError(t *testing.T) {
	st := memory.NewStore()
	fakeSnap := &fakeSnapshotStore{saveError: errors.New("s3 went down")}
	a := NewActivities(st, fakeSnap)

	require.NoError(t, st.SaveFindings(context.Background(), []*types.Finding{
		{ResourceID: "r1", ResourceType: types.ResourceTypeAurora, Status: types.StatusGreen},
	}))

	_, err := runCreateSnapshotActivity(t, a, &CreateSnapshotInput{
		ScanID:        "scan-err",
		ResourceTypes: []types.ResourceType{types.ResourceTypeAurora},
		ScanStartTime: time.Now(),
		ScanEndTime:   time.Now(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "s3 went down")
}

func TestActivities_CreateSnapshot_EmptyFindings(t *testing.T) {
	// No findings in the store and the detection workflow also reported
	// 0 → snapshot is built with zero counts and the activity still
	// succeeds. Mirrors the "successful child but empty inventory" path.
	st := memory.NewStore()
	fakeSnap := &fakeSnapshotStore{}
	a := NewActivities(st, fakeSnap)

	result, err := runCreateSnapshotActivity(t, a, &CreateSnapshotInput{
		ScanID:        "scan-empty",
		ResourceTypes: []types.ResourceType{types.ResourceTypeAurora},
		ScanStartTime: time.Now(),
		ScanEndTime:   time.Now(),
		ExpectedFindingsCounts: map[types.ResourceType]int{
			types.ResourceTypeAurora: 0,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.TotalFindings)
	assert.Equal(t, "scan-empty", result.SnapshotID)
	require.NotNil(t, fakeSnap.saved)
}

func TestActivities_CreateSnapshot_FindingsCountMismatch_FailsWithoutSaving(t *testing.T) {
	// Detection workflow reported 5 aurora findings but the in-memory
	// store has none — this is the "worker died after detection
	// stored findings, snapshot retried on a fresh worker" scenario.
	// The activity must error out and NOT persist anything to S3.
	st := memory.NewStore()
	fakeSnap := &fakeSnapshotStore{}
	a := NewActivities(st, fakeSnap)

	_, err := runCreateSnapshotActivity(t, a, &CreateSnapshotInput{
		ScanID:        "scan-mismatch",
		ResourceTypes: []types.ResourceType{types.ResourceTypeAurora},
		ScanStartTime: time.Now(),
		ScanEndTime:   time.Now(),
		ExpectedFindingsCounts: map[types.ResourceType]int{
			types.ResourceTypeAurora: 5,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "findings count mismatch")
	assert.Contains(t, err.Error(), "expected 5")
	assert.Contains(t, err.Error(), "found 0")

	// And critically: nothing was persisted to S3.
	assert.Equal(t, 0, fakeSnap.saveCallCount, "must not call SaveSnapshot when validation fails")
	assert.Nil(t, fakeSnap.saved)
}
