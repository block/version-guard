package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/block/Version-Guard/pkg/types"
	detection "github.com/block/Version-Guard/pkg/workflow/detection"
)

// newOrchestratorEnv builds a Temporal test environment that mirrors
// the production worker registration: orchestrator workflow + the
// CreateSnapshot activity, with the child detection workflow stubbed
// via OnWorkflow.
func newOrchestratorEnv(t *testing.T) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(OrchestratorWorkflow)
	env.RegisterActivityWithOptions(
		func(_ context.Context) error {
			return nil
		},
		activity.RegisterOptions{Name: ClearFindingsActivityName},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ RecordResourceScanResultInput) error {
			return nil
		},
		activity.RegisterOptions{Name: RecordResourceScanResultActivityName},
	)
	// Detection workflow is registered too so the orchestrator can
	// invoke it as a child by function reference. Its body is stubbed
	// per-test via env.OnWorkflow.
	env.RegisterWorkflow(detection.DetectionWorkflow)
	return env
}

// stubCreateSnapshot registers a fake CreateSnapshot activity that
// returns the supplied result (or an error). Used by every happy-path
// test so they don't all repeat the boilerplate.
func stubCreateSnapshot(env *testsuite.TestWorkflowEnvironment, result *SnapshotResult, returnErr error) {
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ CreateSnapshotInput) (*SnapshotResult, error) {
			if returnErr != nil {
				return nil, returnErr
			}
			return result, nil
		},
		activity.RegisterOptions{Name: CreateSnapshotActivityName},
	)
}

func TestOrchestratorWorkflow_HappyPath(t *testing.T) {
	env := newOrchestratorEnv(t)

	// Stub: every detection child workflow returns 5 findings, all GREEN.
	env.OnWorkflow(detection.DetectionWorkflow, mock.Anything, mock.Anything).
		Return(&detection.WorkflowOutput{
			ScanID:        "scan-1",
			FindingsCount: 5,
			Summary: &types.ScanSummary{
				TotalResources: 5,
				GreenCount:     5,
			},
			DurationMillis: 1000,
		}, nil)

	stubCreateSnapshot(env, &SnapshotResult{
		SnapshotID:           "snap-1",
		TotalFindings:        10,
		CompliancePercentage: 100.0,
	}, nil)

	env.ExecuteWorkflow(OrchestratorWorkflow, WorkflowInput{
		ScanID: "scan-1",
		ResourceTypes: []types.ResourceType{
			types.ResourceTypeAurora,
			types.ResourceTypeEKS,
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var output WorkflowOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	assert.Equal(t, "scan-1", output.ScanID)
	assert.Equal(t, "snap-1", output.SnapshotID)
	assert.Equal(t, 10, output.TotalFindings)
	assert.InDelta(t, 100.0, output.CompliancePercentage, 0.001)
	assert.Len(t, output.ResourceTypeResults, 2)

	for rt, r := range output.ResourceTypeResults {
		assert.Empty(t, r.Error, "resource type %q should have no error", rt)
		assert.Equal(t, 5, r.FindingsCount)
		assert.Equal(t, 5, r.GreenCount)
	}
}

func TestOrchestratorWorkflow_EmptyResourceTypes_ReturnsError(t *testing.T) {
	env := newOrchestratorEnv(t)

	env.ExecuteWorkflow(OrchestratorWorkflow, WorkflowInput{
		ScanID:        "scan-empty",
		ResourceTypes: nil,
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ResourceTypes is empty")
}

func TestOrchestratorWorkflow_ScanIDDefaultsToWorkflowID(t *testing.T) {
	env := newOrchestratorEnv(t)

	env.OnWorkflow(detection.DetectionWorkflow, mock.Anything, mock.Anything).
		Return(&detection.WorkflowOutput{FindingsCount: 1, Summary: &types.ScanSummary{}}, nil)
	stubCreateSnapshot(env, &SnapshotResult{SnapshotID: "snap-default"}, nil)

	env.ExecuteWorkflow(OrchestratorWorkflow, WorkflowInput{
		ScanID:        "", // empty -> orchestrator should populate from workflow ID
		ResourceTypes: []types.ResourceType{types.ResourceTypeAurora},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var output WorkflowOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	assert.NotEmpty(t, output.ScanID, "empty caller ScanID must be filled in by the workflow")
}

func TestOrchestratorWorkflow_PartialChildFailure_ContinuesWithSuccessful(t *testing.T) {
	env := newOrchestratorEnv(t)

	// AURORA succeeds; EKS fails. Routing by ResourceType (not call
	// count) keeps the test stable across the orchestrator's retry
	// policy — Temporal will replay the failing child up to
	// MaximumAttempts before giving up, so a counter-based stub would
	// flip its verdict on retry.
	env.OnWorkflow(detection.DetectionWorkflow, mock.Anything, mock.Anything).
		Return(func(_ workflow.Context, in detection.WorkflowInput) (*detection.WorkflowOutput, error) {
			if in.ResourceType == types.ResourceTypeEKS {
				return nil, errors.New("eks blew up")
			}
			return &detection.WorkflowOutput{
				FindingsCount: 3,
				Summary:       &types.ScanSummary{TotalResources: 3, GreenCount: 3},
			}, nil
		})

	stubCreateSnapshot(env, &SnapshotResult{
		SnapshotID:           "snap-partial",
		TotalFindings:        3,
		CompliancePercentage: 100,
	}, nil)

	env.ExecuteWorkflow(OrchestratorWorkflow, WorkflowInput{
		ScanID: "scan-partial",
		ResourceTypes: []types.ResourceType{
			types.ResourceTypeAurora,
			types.ResourceTypeEKS,
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var output WorkflowOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	assert.Equal(t, "snap-partial", output.SnapshotID)
	assert.Len(t, output.ResourceTypeResults, 2)

	// One result should carry the error; the other should be clean.
	var failed, succeeded int
	for _, r := range output.ResourceTypeResults {
		if r.Error != "" {
			failed++
			assert.Contains(t, r.Error, "eks blew up")
		} else {
			succeeded++
		}
	}
	assert.Equal(t, 1, failed)
	assert.Equal(t, 1, succeeded)
}

func TestOrchestratorWorkflow_FullScanSnapshotInputIncludesExpectedResourceTypes(t *testing.T) {
	env := newOrchestratorEnv(t)

	env.OnWorkflow(detection.DetectionWorkflow, mock.Anything, mock.Anything).
		Return(func(_ workflow.Context, in detection.WorkflowInput) (*detection.WorkflowOutput, error) {
			if in.ResourceType == types.ResourceTypeLambda {
				return nil, errors.New("lambda detector failed")
			}
			return &detection.WorkflowOutput{
				FindingsCount:  3,
				Summary:        &types.ScanSummary{TotalResources: 3, GreenCount: 3},
				DurationMillis: 100,
			}, nil
		})

	var captured CreateSnapshotInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, in CreateSnapshotInput) (*SnapshotResult, error) {
			captured = in
			return &SnapshotResult{
				SnapshotID:           "snap-partial",
				TotalFindings:        3,
				CompliancePercentage: 100,
			}, nil
		},
		activity.RegisterOptions{Name: CreateSnapshotActivityName},
	)

	env.ExecuteWorkflow(OrchestratorWorkflow, WorkflowInput{
		ScanID:    "scan-partial",
		ScanScope: ScanScopeFull,
		ResourceTypes: []types.ResourceType{
			types.ResourceTypeAurora,
			types.ResourceTypeLambda,
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	assert.Equal(t, ScanScopeFull, captured.ScanScope)
	assert.Equal(t, []types.ResourceType{types.ResourceTypeAurora}, captured.ResourceTypes,
		"snapshot should still be built only from successful resource types")
	assert.Equal(t, []types.ResourceType{types.ResourceTypeAurora, types.ResourceTypeLambda}, captured.ExpectedResourceTypes,
		"full-scan validation needs the originally requested resource types")
}

func TestOrchestratorWorkflow_AllChildrenFail_ReturnsError(t *testing.T) {
	env := newOrchestratorEnv(t)

	env.OnWorkflow(detection.DetectionWorkflow, mock.Anything, mock.Anything).
		Return(nil, errors.New("everything is on fire"))

	env.ExecuteWorkflow(OrchestratorWorkflow, WorkflowInput{
		ScanID: "scan-all-fail",
		ResourceTypes: []types.ResourceType{
			types.ResourceTypeAurora,
			types.ResourceTypeEKS,
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all detection workflows failed")
}

func TestOrchestratorWorkflow_SnapshotActivityFailure_BubblesError(t *testing.T) {
	env := newOrchestratorEnv(t)

	env.OnWorkflow(detection.DetectionWorkflow, mock.Anything, mock.Anything).
		Return(&detection.WorkflowOutput{
			FindingsCount: 1,
			Summary:       &types.ScanSummary{TotalResources: 1, GreenCount: 1},
		}, nil)

	// Snapshot activity always fails.
	stubCreateSnapshot(env, nil, errors.New("s3 unavailable"))

	env.ExecuteWorkflow(OrchestratorWorkflow, WorkflowInput{
		ScanID:        "scan-snap-err",
		ResourceTypes: []types.ResourceType{types.ResourceTypeAurora},
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "s3 unavailable")
}
