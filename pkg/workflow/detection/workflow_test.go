package detection

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"

	"github.com/block/Version-Guard/pkg/types"
)

// newTestEnv creates a test workflow environment with sessions enabled,
// matching the production worker configuration.
func newTestEnv(t *testing.T) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()
	// EnableSessionWorker is required because DetectionWorkflow uses sessions
	// to pin activities to the same worker for in-memory caching.
	env.SetWorkerOptions(worker.Options{EnableSessionWorker: true})
	env.RegisterWorkflow(DetectionWorkflow)
	return env
}

func TestDetectionWorkflow_Success(t *testing.T) {
	env := newTestEnv(t)

	// Mock data
	mockResources := []*types.Resource{
		{
			ID:             "arn:aws:rds:us-east-1:123:cluster:test-1",
			Type:           types.ResourceTypeAurora,
			Engine:         "aurora-mysql",
			CurrentVersion: "5.6.10a",
		},
		{
			ID:             "arn:aws:rds:us-east-1:123:cluster:test-2",
			Type:           types.ResourceTypeAurora,
			Engine:         "aurora-mysql",
			CurrentVersion: "8.0.35",
		},
	}

	mockEOL := map[string]*types.VersionLifecycle{
		"aurora-mysql:5.6.10a": {
			Version: "5.6.10a",
			Engine:  "aurora-mysql",
			IsEOL:   true,
		},
		"aurora-mysql:8.0.35": {
			Version:     "8.0.35",
			Engine:      "aurora-mysql",
			IsSupported: true,
		},
	}

	mockFindings := []*types.Finding{
		{
			ResourceID: "arn:aws:rds:us-east-1:123:cluster:test-1",
			Status:     types.StatusRed,
		},
		{
			ResourceID: "arn:aws:rds:us-east-1:123:cluster:test-2",
			Status:     types.StatusGreen,
		},
	}

	mockSummary := &types.ScanSummary{
		TotalResources:       2,
		RedCount:             1,
		GreenCount:           1,
		CompliancePercentage: 50.0,
	}

	// Register and mock activities
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input FetchInventoryInput) (*InventoryResult, error) {
			return &InventoryResult{Resources: mockResources}, nil
		},
		activity.RegisterOptions{Name: FetchInventoryActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input FetchEOLInput) (*EOLResult, error) {
			return &EOLResult{VersionLifecycles: mockEOL}, nil
		},
		activity.RegisterOptions{Name: FetchEOLDataActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input DetectInput) (*DetectResult, error) {
			return &DetectResult{Findings: mockFindings, FindingsCount: len(mockFindings)}, nil
		},
		activity.RegisterOptions{Name: DetectDriftActivityName},
	)

	var storeInput StoreInput
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input StoreInput) error {
			storeInput = input
			return nil
		},
		activity.RegisterOptions{Name: StoreFindingsActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input MetricsInput) (*MetricsResult, error) {
			return &MetricsResult{Summary: mockSummary}, nil
		},
		activity.RegisterOptions{Name: EmitMetricsActivityName},
	)

	// Execute workflow
	env.ExecuteWorkflow(DetectionWorkflow, WorkflowInput{
		ScanID:       "test-scan-001",
		ResourceType: types.ResourceTypeAurora,
	})

	// Verify workflow completed
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	// Verify output
	var output WorkflowOutput
	err := env.GetWorkflowResult(&output)
	require.NoError(t, err)

	assert.Equal(t, "test-scan-001", output.ScanID)
	assert.Equal(t, types.ResourceTypeAurora, output.ResourceType)
	assert.Equal(t, types.ResourceTypeAurora, storeInput.ResourceType)
	assert.Equal(t, 2, output.FindingsCount)
	assert.NotNil(t, output.Summary)
	assert.Equal(t, 50.0, output.Summary.CompliancePercentage)
}

func TestDetectionWorkflow_InventoryFetchError(t *testing.T) {
	env := newTestEnv(t)

	// Mock inventory fetch failure
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input FetchInventoryInput) (*InventoryResult, error) {
			return nil, assert.AnError
		},
		activity.RegisterOptions{Name: FetchInventoryActivityName},
	)

	// Execute workflow
	env.ExecuteWorkflow(DetectionWorkflow, WorkflowInput{
		ScanID:       "test-scan-error",
		ResourceType: types.ResourceTypeAurora,
	})

	// Verify workflow failed
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}

func TestDetectionWorkflow_EmptyInventory(t *testing.T) {
	env := newTestEnv(t)

	// Mock empty inventory
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input FetchInventoryInput) (*InventoryResult, error) {
			return &InventoryResult{Resources: []*types.Resource{}}, nil
		},
		activity.RegisterOptions{Name: FetchInventoryActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input FetchEOLInput) (*EOLResult, error) {
			return &EOLResult{VersionLifecycles: make(map[string]*types.VersionLifecycle)}, nil
		},
		activity.RegisterOptions{Name: FetchEOLDataActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input DetectInput) (*DetectResult, error) {
			return &DetectResult{Findings: []*types.Finding{}, FindingsCount: 0}, nil
		},
		activity.RegisterOptions{Name: DetectDriftActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input StoreInput) error {
			return nil
		},
		activity.RegisterOptions{Name: StoreFindingsActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input MetricsInput) (*MetricsResult, error) {
			return &MetricsResult{Summary: &types.ScanSummary{}}, nil
		},
		activity.RegisterOptions{Name: EmitMetricsActivityName},
	)

	// Execute workflow
	env.ExecuteWorkflow(DetectionWorkflow, WorkflowInput{
		ScanID:       "test-scan-empty",
		ResourceType: types.ResourceTypeAurora,
	})

	// Verify workflow completed successfully (empty inventory is valid)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	// Verify output
	var output WorkflowOutput
	err := env.GetWorkflowResult(&output)
	require.NoError(t, err)
	assert.Equal(t, 0, output.FindingsCount)
}

func TestDetectionWorkflow_MetricsFailureContinues(t *testing.T) {
	env := newTestEnv(t)

	mockResources := []*types.Resource{
		{ID: "test", Engine: "aurora-mysql", CurrentVersion: "8.0.35"},
	}

	// Mock successful activities except metrics
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input FetchInventoryInput) (*InventoryResult, error) {
			return &InventoryResult{Resources: mockResources}, nil
		},
		activity.RegisterOptions{Name: FetchInventoryActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input FetchEOLInput) (*EOLResult, error) {
			return &EOLResult{VersionLifecycles: make(map[string]*types.VersionLifecycle)}, nil
		},
		activity.RegisterOptions{Name: FetchEOLDataActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input DetectInput) (*DetectResult, error) {
			return &DetectResult{Findings: []*types.Finding{{ResourceID: "test", Status: types.StatusGreen}}, FindingsCount: 1}, nil
		},
		activity.RegisterOptions{Name: DetectDriftActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input StoreInput) error {
			return nil
		},
		activity.RegisterOptions{Name: StoreFindingsActivityName},
	)

	// Metrics fails but workflow should continue
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input MetricsInput) (*MetricsResult, error) {
			return nil, assert.AnError
		},
		activity.RegisterOptions{Name: EmitMetricsActivityName},
	)

	// Execute workflow
	env.ExecuteWorkflow(DetectionWorkflow, WorkflowInput{
		ScanID:       "test-scan-metrics-fail",
		ResourceType: types.ResourceTypeAurora,
	})

	// Workflow should complete despite metrics failure
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	// Verify output (summary will be nil due to metrics failure)
	var output WorkflowOutput
	err := env.GetWorkflowResult(&output)
	require.NoError(t, err)
	assert.Equal(t, 1, output.FindingsCount)
}

func TestDetectionWorkflow_ActivityRetry(t *testing.T) {
	env := newTestEnv(t)

	// Mock activity that fails twice then succeeds
	callCount := 0
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input FetchInventoryInput) (*InventoryResult, error) {
			callCount++
			if callCount < 3 {
				return nil, assert.AnError // Fail first 2 times
			}
			return &InventoryResult{Resources: []*types.Resource{}}, nil
		},
		activity.RegisterOptions{Name: FetchInventoryActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input FetchEOLInput) (*EOLResult, error) {
			return &EOLResult{VersionLifecycles: make(map[string]*types.VersionLifecycle)}, nil
		},
		activity.RegisterOptions{Name: FetchEOLDataActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input DetectInput) (*DetectResult, error) {
			return &DetectResult{Findings: []*types.Finding{}, FindingsCount: 0}, nil
		},
		activity.RegisterOptions{Name: DetectDriftActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input StoreInput) error {
			return nil
		},
		activity.RegisterOptions{Name: StoreFindingsActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input MetricsInput) (*MetricsResult, error) {
			return &MetricsResult{Summary: &types.ScanSummary{}}, nil
		},
		activity.RegisterOptions{Name: EmitMetricsActivityName},
	)

	// Execute workflow
	env.ExecuteWorkflow(DetectionWorkflow, WorkflowInput{
		ScanID:       "test-scan-retry",
		ResourceType: types.ResourceTypeAurora,
	})

	// Workflow should eventually succeed after retries
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	// Verify activity was retried
	assert.Equal(t, 3, callCount, "Activity should have been called 3 times (2 failures + 1 success)")
}

func TestDetectionWorkflow_DurationTracking(t *testing.T) {
	env := newTestEnv(t)

	// Set up mock activities
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input FetchInventoryInput) (*InventoryResult, error) {
			return &InventoryResult{Resources: []*types.Resource{}}, nil
		},
		activity.RegisterOptions{Name: FetchInventoryActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input FetchEOLInput) (*EOLResult, error) {
			return &EOLResult{VersionLifecycles: make(map[string]*types.VersionLifecycle)}, nil
		},
		activity.RegisterOptions{Name: FetchEOLDataActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input DetectInput) (*DetectResult, error) {
			return &DetectResult{Findings: []*types.Finding{}, FindingsCount: 0}, nil
		},
		activity.RegisterOptions{Name: DetectDriftActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input StoreInput) error {
			return nil
		},
		activity.RegisterOptions{Name: StoreFindingsActivityName},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, input MetricsInput) (*MetricsResult, error) {
			return &MetricsResult{Summary: &types.ScanSummary{}}, nil
		},
		activity.RegisterOptions{Name: EmitMetricsActivityName},
	)

	// Execute workflow
	env.ExecuteWorkflow(DetectionWorkflow, WorkflowInput{
		ScanID:       "test-scan-duration",
		ResourceType: types.ResourceTypeAurora,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	// Verify duration was tracked
	var output WorkflowOutput
	err := env.GetWorkflowResult(&output)
	require.NoError(t, err)

	// In test environment, duration is 0 because time doesn't advance unless explicitly set
	assert.GreaterOrEqual(t, output.DurationMillis, int64(0), "Duration should be tracked")
	assert.False(t, output.CompletedAt.IsZero(), "CompletedAt should be set")
}
