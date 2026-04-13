package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	"github.com/block/Version-Guard/pkg/workflow/orchestrator"
)

// Config holds configuration for the Temporal schedule.
type Config struct {
	ScheduleID     string
	CronExpression string
	TaskQueue      string
	Jitter         time.Duration
	Enabled        bool
	Paused         bool
}

// Creator abstracts the Temporal schedule client for testability.
type Creator interface {
	Create(ctx context.Context, options client.ScheduleOptions) (client.ScheduleHandle, error)
	GetHandle(ctx context.Context, scheduleID string) client.ScheduleHandle
}

// Manager handles Temporal schedule lifecycle.
type Manager struct {
	scheduleClient Creator
}

// NewManager creates a Manager from a Temporal client.
func NewManager(c client.Client) *Manager {
	return &Manager{scheduleClient: c.ScheduleClient()}
}

// NewManagerWithClient creates a Manager with an explicit Creator (for testing).
func NewManagerWithClient(sc Creator) *Manager {
	return &Manager{scheduleClient: sc}
}

// EnsureSchedule creates the schedule if it doesn't exist, or updates it
// if the cron expression has changed.
func (m *Manager) EnsureSchedule(ctx context.Context, cfg Config) error {
	if !cfg.Enabled {
		return nil
	}

	opts := client.ScheduleOptions{
		ID: cfg.ScheduleID,
		Spec: client.ScheduleSpec{
			CronExpressions: []string{cfg.CronExpression},
			Jitter:          cfg.Jitter,
		},
		Action: &client.ScheduleWorkflowAction{
			Workflow:                 orchestrator.OrchestratorWorkflow,
			Args:                     []interface{}{orchestrator.WorkflowInput{}},
			TaskQueue:                cfg.TaskQueue,
			WorkflowExecutionTimeout: 2 * time.Hour,
		},
		Paused: cfg.Paused,
	}

	_, err := m.scheduleClient.Create(ctx, opts)
	if err == nil {
		return nil
	}

	// If the schedule already exists, check if we need to update it
	if !isScheduleAlreadyRunning(err) {
		return fmt.Errorf("failed to create schedule %q: %w", cfg.ScheduleID, err)
	}

	handle := m.scheduleClient.GetHandle(ctx, cfg.ScheduleID)
	desc, err := handle.Describe(ctx)
	if err != nil {
		return fmt.Errorf("failed to describe existing schedule %q: %w", cfg.ScheduleID, err)
	}

	// Check if the cron expression or jitter has changed
	existingSpec := desc.Schedule.Spec
	if existingSpec == nil {
		existingSpec = &client.ScheduleSpec{}
	}
	existingCrons := existingSpec.CronExpressions
	if len(existingCrons) == 1 && existingCrons[0] == cfg.CronExpression && existingSpec.Jitter == cfg.Jitter {
		fmt.Printf("  Schedule %q already configured (cron: %s)\n", cfg.ScheduleID, cfg.CronExpression)
		return nil
	}

	// Update the schedule with the new spec
	err = handle.Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			if input.Description.Schedule.Spec == nil {
				input.Description.Schedule.Spec = &client.ScheduleSpec{}
			}
			input.Description.Schedule.Spec.CronExpressions = []string{cfg.CronExpression}
			input.Description.Schedule.Spec.Jitter = cfg.Jitter
			if action, ok := input.Description.Schedule.Action.(*client.ScheduleWorkflowAction); ok {
				action.TaskQueue = cfg.TaskQueue
			}
			return &client.ScheduleUpdate{
				Schedule: &input.Description.Schedule,
			}, nil
		},
	})
	if err != nil {
		return fmt.Errorf("failed to update schedule %q: %w", cfg.ScheduleID, err)
	}

	fmt.Printf("  Schedule %q updated (cron: %s)\n", cfg.ScheduleID, cfg.CronExpression)
	return nil
}

func isScheduleAlreadyRunning(err error) bool {
	return errors.Is(err, temporal.ErrScheduleAlreadyRunning)
}
