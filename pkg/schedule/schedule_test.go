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
)

// mockScheduleHandle implements client.ScheduleHandle for testing.
type mockScheduleHandle struct {
	id           string
	describeOut  *client.ScheduleDescription
	describeErr  error
	updateErr    error
	updateCalled bool
	updateFn     func(client.ScheduleUpdateOptions)
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

// mockScheduleCreator implements ScheduleCreator for testing.
type mockScheduleCreator struct {
	createErr    error
	createHandle client.ScheduleHandle
	handle       *mockScheduleHandle
	createOpts   *client.ScheduleOptions
}

func (c *mockScheduleCreator) Create(_ context.Context, opts client.ScheduleOptions) (client.ScheduleHandle, error) {
	c.createOpts = &opts
	return c.createHandle, c.createErr
}

func (c *mockScheduleCreator) GetHandle(_ context.Context, _ string) client.ScheduleHandle {
	return c.handle
}

func TestEnsureSchedule_Disabled(t *testing.T) {
	mock := &mockScheduleCreator{}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), ScheduleConfig{
		Enabled: false,
	})

	require.NoError(t, err)
	assert.Nil(t, mock.createOpts, "Create should not be called when disabled")
}

func TestEnsureSchedule_CreatesNew(t *testing.T) {
	mock := &mockScheduleCreator{
		createHandle: &mockScheduleHandle{id: "test-schedule"},
	}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), ScheduleConfig{
		Enabled:        true,
		ScheduleID:     "test-schedule",
		CronExpression: "0 */6 * * *",
		Jitter:         5 * time.Minute,
		TaskQueue:      "test-queue",
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

func TestEnsureSchedule_AlreadyExists_SameCron(t *testing.T) {
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
	mock := &mockScheduleCreator{
		createErr: temporal.ErrScheduleAlreadyRunning,
		handle:    handle,
	}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), ScheduleConfig{
		Enabled:        true,
		ScheduleID:     "test-schedule",
		CronExpression: "0 */6 * * *",
		Jitter:         5 * time.Minute,
		TaskQueue:      "test-queue",
	})

	require.NoError(t, err)
	assert.False(t, handle.updateCalled, "Update should not be called when cron matches")
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
	mock := &mockScheduleCreator{
		createErr: temporal.ErrScheduleAlreadyRunning,
		handle:    handle,
	}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), ScheduleConfig{
		Enabled:        true,
		ScheduleID:     "test-schedule",
		CronExpression: "0 */6 * * *",
		Jitter:         5 * time.Minute,
		TaskQueue:      "test-queue",
	})

	require.NoError(t, err)
	assert.True(t, handle.updateCalled, "Update should be called when cron differs")
}

func TestEnsureSchedule_CreateError(t *testing.T) {
	mock := &mockScheduleCreator{
		createErr: errors.New("connection refused"),
	}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), ScheduleConfig{
		Enabled:        true,
		ScheduleID:     "test-schedule",
		CronExpression: "0 */6 * * *",
		TaskQueue:      "test-queue",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestEnsureSchedule_DescribeError(t *testing.T) {
	handle := &mockScheduleHandle{
		id:          "test-schedule",
		describeErr: errors.New("not found"),
	}
	mock := &mockScheduleCreator{
		createErr: temporal.ErrScheduleAlreadyRunning,
		handle:    handle,
	}
	mgr := NewManagerWithClient(mock)

	err := mgr.EnsureSchedule(context.Background(), ScheduleConfig{
		Enabled:        true,
		ScheduleID:     "test-schedule",
		CronExpression: "0 */6 * * *",
		TaskQueue:      "test-queue",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
