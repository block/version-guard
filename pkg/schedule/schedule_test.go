package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	"github.com/block/Version-Guard/pkg/types"
	"github.com/block/Version-Guard/pkg/workflow/orchestrator"
)

// testResourceTypes is the canonical fixture list for ResourceTypes —
// EnsureSchedule now rejects empty ResourceTypes (the orchestrator
// workflow has no hardcoded fallback list), so every test that expects
// the create/update path to run must pass a non-empty list.
var testResourceTypes = []types.ResourceType{"aurora-mysql", "eks"}

// mockScheduleHandle implements client.ScheduleHandle for testing.
type mockScheduleHandle struct {
	describeErr  error
	updateErr    error
	updateFn     func(client.ScheduleUpdateOptions)
	describeOut  *client.ScheduleDescription
	id           string
	updateCalled bool
}

func (h *mockScheduleHandle) GetID() string                  { return h.id }
func (h *mockScheduleHandle) Delete(_ context.Context) error { return nil }
func (h *mockScheduleHandle) Backfill(_ context.Context, _ client.ScheduleBackfillOptions) error {
	return nil
}
func (h *mockScheduleHandle) Trigger(_ context.Context, _ client.ScheduleTriggerOptions) error {
	return nil
}
func (h *mockScheduleHandle) Pause(_ context.Context, _ client.SchedulePauseOptions) error {
	return nil
}
func (h *mockScheduleHandle) Unpause(_ context.Context, _ client.ScheduleUnpauseOptions) error {
	return nil
}

func (h *mockScheduleHandle) Describe(_ context.Context) (*client.ScheduleDescription, error) {
	return h.describeOut, h.describeErr
}

func (h *mockScheduleHandle) Update(_ context.Context, opts client.ScheduleUpdateOptions) error {
	h.updateCalled = true
	if h.updateFn != nil {
		h.updateFn(opts)
	}
	return h.updateErr
}

// mockCreator implements Creator for testing.
type mockCreator struct {
	createErr    error
	createHandle client.ScheduleHandle
	handle       *mockScheduleHandle
	createOpts   *client.ScheduleOptions
}

func (c *mockCreator) Create(_ context.Context, opts client.ScheduleOptions) (client.ScheduleHandle, error) { //nolint:gocritic // matches SDK interface
	c.createOpts = &opts
	return c.createHandle, c.createErr
}

func (c *mockCreator) GetHandle(_ context.Context, _ string) client.ScheduleHandle {
	return c.handle
}

func TestEnsureSchedule_Disabled(t *testing.T) {
	mock := &mockCreator{}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), Config{
		Enabled: false,
	})

	require.NoError(t, err)
	assert.Nil(t, mock.createOpts, "Create should not be called when disabled")
}

// TestEnsureSchedule_EmptyResourceTypes_Rejected guards the contract that
// the orchestrator workflow no longer carries a hardcoded fallback list:
// scheduled runs must declare an explicit ResourceTypes list (sourced
// from the loaded YAML config at startup), otherwise the schedule would
// fire and immediately fail with ErrNoResourceTypes every cron tick.
func TestEnsureSchedule_EmptyResourceTypes_Rejected(t *testing.T) {
	mock := &mockCreator{}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), Config{
		Enabled:        true,
		ScheduleID:     "test-schedule",
		CronExpression: "0 */6 * * *",
		TaskQueue:      "test-queue",
		// ResourceTypes intentionally omitted
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ResourceTypes is empty")
	assert.Nil(t, mock.createOpts, "Create must not be called when ResourceTypes is empty")
}

func TestEnsureSchedule_CreatesNew(t *testing.T) {
	mock := &mockCreator{
		createHandle: &mockScheduleHandle{id: "test-schedule"},
	}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), Config{
		Enabled:        true,
		ScheduleID:     "test-schedule",
		CronExpression: "0 */6 * * *",
		Jitter:         5 * time.Minute,
		TaskQueue:      "test-queue",
		ResourceTypes:  testResourceTypes,
	})

	require.NoError(t, err)
	require.NotNil(t, mock.createOpts)
	assert.Equal(t, "test-schedule", mock.createOpts.ID)
	assert.Equal(t, []string{"0 */6 * * *"}, mock.createOpts.Spec.CronExpressions)
	assert.Equal(t, 5*time.Minute, mock.createOpts.Spec.Jitter)
	action := mock.createOpts.Action.(*client.ScheduleWorkflowAction)
	assert.Equal(t, "test-queue", action.TaskQueue)
	assert.Equal(t, 2*time.Hour, action.WorkflowExecutionTimeout)
}

// TestEnsureSchedule_AlreadyExists_AlwaysUpdates guards the contract
// that an existing schedule is unconditionally refreshed on every
// startup. The previous "skip when cron+jitter match" optimization
// failed to refresh Action.Args, leaving stale ResourceTypes baked
// into pre-existing schedules — see the doc comment on the update
// path in schedule.go for the full incident background.
func TestEnsureSchedule_AlreadyExists_AlwaysUpdates(t *testing.T) {
	handle := &mockScheduleHandle{
		id: "test-schedule",
		describeOut: &client.ScheduleDescription{
			Schedule: client.Schedule{
				Spec: &client.ScheduleSpec{
					CronExpressions: []string{"0 */6 * * *"},
					Jitter:          5 * time.Minute,
				},
			},
		},
	}
	mock := &mockCreator{
		createErr: temporal.ErrScheduleAlreadyRunning,
		handle:    handle,
	}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), Config{
		Enabled:        true,
		ScheduleID:     "test-schedule",
		CronExpression: "0 */6 * * *",
		Jitter:         5 * time.Minute,
		TaskQueue:      "test-queue",
		ResourceTypes:  testResourceTypes,
	})

	require.NoError(t, err)
	assert.True(t, handle.updateCalled,
		"Update must always run on existing schedules so Action.Args is refreshed")
}

func TestEnsureSchedule_AlreadyExists_DifferentCron(t *testing.T) {
	handle := &mockScheduleHandle{
		id: "test-schedule",
		describeOut: &client.ScheduleDescription{
			Schedule: client.Schedule{
				Spec: &client.ScheduleSpec{
					CronExpressions: []string{"0 */12 * * *"},
					Jitter:          5 * time.Minute,
				},
			},
		},
	}
	mock := &mockCreator{
		createErr: temporal.ErrScheduleAlreadyRunning,
		handle:    handle,
	}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), Config{
		Enabled:        true,
		ScheduleID:     "test-schedule",
		CronExpression: "0 */6 * * *",
		Jitter:         5 * time.Minute,
		TaskQueue:      "test-queue",
		ResourceTypes:  testResourceTypes,
	})

	require.NoError(t, err)
	assert.True(t, handle.updateCalled, "Update should be called when cron differs")
}

func TestEnsureSchedule_CreateError(t *testing.T) {
	mock := &mockCreator{
		createErr: errors.New("connection refused"),
	}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), Config{
		Enabled:        true,
		ScheduleID:     "test-schedule",
		CronExpression: "0 */6 * * *",
		TaskQueue:      "test-queue",
		ResourceTypes:  testResourceTypes,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestEnsureSchedule_AlreadyExists_NilSpec(t *testing.T) {
	handle := &mockScheduleHandle{
		id: "test-schedule",
		describeOut: &client.ScheduleDescription{
			Schedule: client.Schedule{
				Spec: nil, // nil Spec in describe output
			},
		},
	}
	// Capture and invoke the DoUpdate callback to verify the nil Spec guard
	// inside the update path doesn't panic.
	handle.updateFn = func(opts client.ScheduleUpdateOptions) {
		// Simulate what the real Temporal SDK does: call DoUpdate with
		// the described schedule (which has a nil Spec).
		input := client.ScheduleUpdateInput{
			Description: *handle.describeOut,
		}
		result, err := opts.DoUpdate(input)
		require.NoError(t, err, "DoUpdate should not error with nil Spec")
		require.NotNil(t, result, "DoUpdate should return an update")
		require.NotNil(t, result.Schedule.Spec, "Spec should be non-nil after DoUpdate sets it")
		assert.Equal(t, []string{"0 */6 * * *"}, result.Schedule.Spec.CronExpressions)
		assert.Equal(t, 5*time.Minute, result.Schedule.Spec.Jitter)
	}
	mock := &mockCreator{
		createErr: temporal.ErrScheduleAlreadyRunning,
		handle:    handle,
	}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), Config{
		Enabled:        true,
		ScheduleID:     "test-schedule",
		CronExpression: "0 */6 * * *",
		Jitter:         5 * time.Minute,
		TaskQueue:      "test-queue",
		ResourceTypes:  testResourceTypes,
	})

	require.NoError(t, err)
	assert.True(t, handle.updateCalled, "Update should be called when Spec is nil")
}

// TestEnsureSchedule_Update_ReplacesStaleCalendars guards against a bug where
// Temporal's describe() returns the parsed cron inside Calendars with an empty
// CronExpressions field. Mutating only CronExpressions on update would leave
// stale Calendars in place, causing the schedule to fire on both the old and
// new crons after a restart with a changed schedule.
func TestEnsureSchedule_Update_ReplacesStaleCalendars(t *testing.T) {
	handle := &mockScheduleHandle{
		id: "test-schedule",
		describeOut: &client.ScheduleDescription{
			Schedule: client.Schedule{
				Spec: &client.ScheduleSpec{
					// Simulate what a real Temporal server returns after a
					// previous create: cron parsed into Calendars, CronExpressions empty.
					Calendars: []client.ScheduleCalendarSpec{
						{Minute: []client.ScheduleRange{{Start: 0, End: 59, Step: 2}}},
					},
					Jitter: 30 * time.Second,
				},
			},
		},
	}
	var captured *client.ScheduleUpdate
	handle.updateFn = func(opts client.ScheduleUpdateOptions) {
		input := client.ScheduleUpdateInput{Description: *handle.describeOut}
		result, err := opts.DoUpdate(input)
		require.NoError(t, err)
		captured = result
	}
	mock := &mockCreator{
		createErr: temporal.ErrScheduleAlreadyRunning,
		handle:    handle,
	}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), Config{
		Enabled:        true,
		ScheduleID:     "test-schedule",
		CronExpression: "*/5 * * * *",
		Jitter:         1 * time.Minute,
		TaskQueue:      "test-queue",
		ResourceTypes:  testResourceTypes,
	})

	require.NoError(t, err)
	require.NotNil(t, captured)
	require.NotNil(t, captured.Schedule.Spec)
	assert.Empty(t, captured.Schedule.Spec.Calendars,
		"stale Calendars must be cleared on update to prevent cron stacking")
	assert.Equal(t, []string{"*/5 * * * *"}, captured.Schedule.Spec.CronExpressions)
	assert.Equal(t, 1*time.Minute, captured.Schedule.Spec.Jitter)
}

// TestEnsureSchedule_Update_RefreshesActionArgs is the regression
// guard for the silent-arg-drift bug. Before the fix, the update path
// only rewrote Spec and left Action.Args untouched, so a schedule
// created on an older code revision (with empty/stale ResourceTypes)
// kept the stale args forever — every cron firing then failed
// instantly with ErrNoResourceTypes. This test verifies that
// EnsureSchedule overwrites Action.Args (and TaskQueue) with the
// current cfg values whenever it touches an existing schedule.
func TestEnsureSchedule_Update_RefreshesActionArgs(t *testing.T) {
	staleResourceTypes := []types.ResourceType{"old-resource-from-prior-revision"}
	handle := &mockScheduleHandle{
		id: "test-schedule",
		describeOut: &client.ScheduleDescription{
			Schedule: client.Schedule{
				Spec: &client.ScheduleSpec{
					CronExpressions: []string{"0 6 * * *"},
				},
				Action: &client.ScheduleWorkflowAction{
					TaskQueue: "old-task-queue",
					Args: []interface{}{
						// Simulate a stale args payload from an earlier
						// schedule revision that had different
						// ResourceTypes than the current YAML config.
						map[string]interface{}{
							"ScanID":        "",
							"ResourceTypes": staleResourceTypes,
						},
					},
				},
			},
		},
	}
	var captured *client.ScheduleUpdate
	handle.updateFn = func(opts client.ScheduleUpdateOptions) {
		input := client.ScheduleUpdateInput{Description: *handle.describeOut}
		result, err := opts.DoUpdate(input)
		require.NoError(t, err)
		captured = result
	}
	mock := &mockCreator{
		createErr: temporal.ErrScheduleAlreadyRunning,
		handle:    handle,
	}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), Config{
		Enabled:        true,
		ScheduleID:     "test-schedule",
		CronExpression: "0 6 * * *",
		TaskQueue:      "new-task-queue",
		ResourceTypes:  testResourceTypes,
	})

	require.NoError(t, err)
	require.NotNil(t, captured)
	action, ok := captured.Schedule.Action.(*client.ScheduleWorkflowAction)
	require.True(t, ok, "Action must remain a ScheduleWorkflowAction")
	assert.Equal(t, "new-task-queue", action.TaskQueue,
		"TaskQueue must be refreshed from current cfg")
	require.Len(t, action.Args, 1, "Args must be exactly the orchestrator WorkflowInput")
	input, ok := action.Args[0].(orchestrator.WorkflowInput)
	require.True(t, ok, "Args[0] must be a typed orchestrator.WorkflowInput, not the stale payload")
	assert.Equal(t, testResourceTypes, input.ResourceTypes,
		"ResourceTypes must be refreshed from current cfg, not preserved from the stale schedule")
}
