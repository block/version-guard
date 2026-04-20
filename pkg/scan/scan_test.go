package scan

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	"github.com/block/Version-Guard/pkg/types"
	"github.com/block/Version-Guard/pkg/workflow/orchestrator"
)

// mockWorkflowRun implements client.WorkflowRun for testing.
type mockWorkflowRun struct {
	id    string
	runID string
}

func (r *mockWorkflowRun) GetID() string    { return r.id }
func (r *mockWorkflowRun) GetRunID() string { return r.runID }
func (r *mockWorkflowRun) Get(_ context.Context, _ interface{}) error {
	return nil
}
func (r *mockWorkflowRun) GetWithOptions(_ context.Context, _ interface{}, _ client.WorkflowRunGetOptions) error {
	return nil
}

// mockStarter captures the arguments to ExecuteWorkflow.
type mockStarter struct {
	err        error
	run        client.WorkflowRun
	calledOpts client.StartWorkflowOptions
	calledArgs []interface{}
	called     bool
}

func (m *mockStarter) ExecuteWorkflow(_ context.Context, options client.StartWorkflowOptions, _ interface{}, args ...interface{}) (client.WorkflowRun, error) { //nolint:gocritic // matches SDK interface
	m.called = true
	m.calledOpts = options
	m.calledArgs = args
	if m.err != nil {
		return nil, m.err
	}
	return m.run, nil
}

func TestTrigger_Run_FullScan(t *testing.T) {
	mock := &mockStarter{
		run: &mockWorkflowRun{id: "version-guard-scan-abc", runID: "run-1"},
	}
	tr := NewTriggerWithStarter(mock, "version-guard-orchestrator")

	res, err := tr.Run(context.Background(), Input{ScanID: "abc"})

	require.NoError(t, err)
	assert.Equal(t, "version-guard-scan-abc", res.WorkflowID)
	assert.Equal(t, "run-1", res.RunID)
	assert.Equal(t, "abc", res.ScanID)

	require.True(t, mock.called)
	assert.Equal(t, "version-guard-scan-abc", mock.calledOpts.ID)
	assert.Equal(t, "version-guard-orchestrator", mock.calledOpts.TaskQueue)

	require.Len(t, mock.calledArgs, 1)
	in, ok := mock.calledArgs[0].(orchestrator.WorkflowInput)
	require.True(t, ok, "workflow args[0] should be orchestrator.WorkflowInput")
	assert.Equal(t, "abc", in.ScanID)
	assert.Empty(t, in.ResourceTypes, "empty ResourceTypes means full scan")
}

func TestTrigger_Run_TargetedScan(t *testing.T) {
	mock := &mockStarter{
		run: &mockWorkflowRun{id: "wf", runID: "run"},
	}
	tr := NewTriggerWithStarter(mock, "version-guard-orchestrator")

	targets := []types.ResourceType{"aurora-mysql", "eks"}
	_, err := tr.Run(context.Background(), Input{
		ScanID:        "targeted-1",
		ResourceTypes: targets,
	})

	require.NoError(t, err)
	require.Len(t, mock.calledArgs, 1)
	in := mock.calledArgs[0].(orchestrator.WorkflowInput)
	assert.Equal(t, targets, in.ResourceTypes)
}

func TestTrigger_Run_GeneratesScanIDWhenEmpty(t *testing.T) {
	mock := &mockStarter{
		run: &mockWorkflowRun{id: "wf", runID: "run"},
	}
	tr := NewTriggerWithStarter(mock, "version-guard-orchestrator")

	res, err := tr.Run(context.Background(), Input{})

	require.NoError(t, err)
	assert.NotEmpty(t, res.ScanID, "ScanID should be generated when not provided")
	assert.True(t, strings.HasPrefix(mock.calledOpts.ID, "version-guard-scan-"),
		"workflow ID should be prefixed")
	in := mock.calledArgs[0].(orchestrator.WorkflowInput)
	assert.Equal(t, res.ScanID, in.ScanID, "generated ScanID should be passed to workflow")
}

func TestTrigger_Run_ReturnsErrorWhenTaskQueueMissing(t *testing.T) {
	mock := &mockStarter{}
	tr := NewTriggerWithStarter(mock, "")

	_, err := tr.Run(context.Background(), Input{})

	require.Error(t, err)
	assert.False(t, mock.called, "ExecuteWorkflow must not be called with empty task queue")
}

func TestNewTrigger_WiresClientAsStarter(t *testing.T) {
	// client.Client satisfies the Starter interface; NewTrigger is a thin
	// constructor that stores it. Passing nil is enough to exercise the line —
	// we only assert the fields are wired.
	tr := NewTrigger(nil, "version-guard-orchestrator")

	require.NotNil(t, tr)
	assert.Equal(t, "version-guard-orchestrator", tr.taskQueue)
	assert.Nil(t, tr.starter, "nil client should pass through as nil Starter")
}

func TestTrigger_Run_PropagatesStarterError(t *testing.T) {
	wantErr := errors.New("temporal unavailable")
	mock := &mockStarter{err: wantErr}
	tr := NewTriggerWithStarter(mock, "version-guard-orchestrator")

	_, err := tr.Run(context.Background(), Input{ScanID: "x"})

	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}
