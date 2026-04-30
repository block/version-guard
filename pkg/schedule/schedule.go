package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	"github.com/block/Version-Guard/pkg/types"
	"github.com/block/Version-Guard/pkg/workflow/orchestrator"
)

// Config holds configuration for the Temporal schedule.
type Config struct {
	ScheduleID     string
	CronExpression string
	TaskQueue      string
	// ResourceTypes is the list of resource config IDs to scan on each
	// scheduled run. Sourced from the loaded YAML config at startup —
	// empty is rejected by the orchestrator workflow because there is
	// no longer a hardcoded fallback list (see
	// orchestrator.ErrNoResourceTypes).
	ResourceTypes []types.ResourceType
	Jitter        time.Duration
	Enabled       bool
	Paused        bool
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
//
//nolint:gocritic // Config is a startup-time, called-once value; pass-by-value keeps callers (cmd/server) free of pointer ceremony.
func (m *Manager) EnsureSchedule(ctx context.Context, cfg Config) error {
	if !cfg.Enabled {
		return nil
	}

	if len(cfg.ResourceTypes) == 0 {
		return fmt.Errorf("schedule %q: ResourceTypes is empty; populate from loaded config so scheduled runs aren't no-ops", cfg.ScheduleID)
	}

	opts := client.ScheduleOptions{
		ID: cfg.ScheduleID,
		Spec: client.ScheduleSpec{
			CronExpressions: []string{cfg.CronExpression},
			Jitter:          cfg.Jitter,
		},
		Action: &client.ScheduleWorkflowAction{
			Workflow: orchestrator.OrchestratorWorkflow,
			Args: []interface{}{orchestrator.WorkflowInput{
				ResourceTypes: cfg.ResourceTypes,
			}},
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

	// Always refresh existing schedules with the current Spec AND Action
	// (Args + TaskQueue) on every startup. The previous "skip when
	// cron+jitter match" optimization was unsafe: it only diffed Spec
	// and never touched Action.Args, so a schedule created on an older
	// code revision (when the orchestrator carried a hardcoded
	// fallback resource list) kept a now-stale ResourceTypes:null in
	// its Args forever. After the orchestrator started rejecting empty
	// ResourceTypes (ErrNoResourceTypes), every cron firing failed
	// instantly with no log past "Starting orchestrator workflow".
	//
	// Args are encoded as opaque payloads in the Temporal Schedule, so
	// we cannot reliably diff them against cfg.ResourceTypes here.
	// One Update RPC per pod startup is a trivial cost compared to the
	// outage risk of silent arg drift; rebuild the schedule
	// unconditionally and let Temporal handle the no-op case.
	//
	// We replace the entire Spec rather than mutating fields because
	// Temporal parses CronExpressions into Calendars/StructuredCalendar
	// server-side on create. On subsequent describes, the cron lives
	// in Calendars and CronExpressions comes back empty — mutating
	// CronExpressions alone would leave stale calendars in place,
	// causing the schedule to fire on both the old and new cadences
	// after every restart with a changed cron.
	err = handle.Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			input.Description.Schedule.Spec = &client.ScheduleSpec{
				CronExpressions: []string{cfg.CronExpression},
				Jitter:          cfg.Jitter,
			}
			if action, ok := input.Description.Schedule.Action.(*client.ScheduleWorkflowAction); ok {
				action.TaskQueue = cfg.TaskQueue
				action.Args = []interface{}{orchestrator.WorkflowInput{
					ResourceTypes: cfg.ResourceTypes,
				}}
			}
			return &client.ScheduleUpdate{
				Schedule: &input.Description.Schedule,
			}, nil
		},
	})
	if err != nil {
		return fmt.Errorf("failed to update schedule %q: %w", cfg.ScheduleID, err)
	}

	fmt.Printf("  Schedule %q refreshed (cron: %s, resources: %d)\n",
		cfg.ScheduleID, cfg.CronExpression, len(cfg.ResourceTypes))
	return nil
}

func isScheduleAlreadyRunning(err error) bool {
	return errors.Is(err, temporal.ErrScheduleAlreadyRunning)
}
