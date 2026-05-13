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
// Field order is tuned for govet fieldalignment: all string fields
// before the slice keeps the pointer span minimal.
type Config struct {
	ScheduleID     string
	CronExpression string
	TaskQueue      string
	// EmitterWebhookURL, when non-empty, is forwarded into every
	// scheduled OrchestratorWorkflow run so it can fire the
	// notify activity once the snapshot is persisted. Empty disables
	// the webhook for scheduled runs.
	EmitterWebhookURL string
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
				ResourceTypes:     cfg.ResourceTypes,
				EmitterWebhookURL: cfg.EmitterWebhookURL,
				ScanScope:         orchestrator.ScanScopeFull,
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
	desc, err := handle.Describe(ctx)
	if err != nil {
		return fmt.Errorf("failed to describe existing schedule %q: %w", cfg.ScheduleID, err)
	}

	// Check if anything observable has changed: spec (cron/jitter) or
	// the workflow action's task queue / WorkflowInput. We must compare
	// the action's WorkflowInput too — otherwise propagating a newly
	// set EmitterWebhookURL (or a changed ResourceTypes list) would
	// silently no-op on upgraded deployments where the schedule already
	// exists with stale args.
	existingSpec := desc.Schedule.Spec
	if existingSpec == nil {
		existingSpec = &client.ScheduleSpec{}
	}
	existingCrons := existingSpec.CronExpressions
	specMatches := len(existingCrons) == 1 && existingCrons[0] == cfg.CronExpression && existingSpec.Jitter == cfg.Jitter
	actionMatches := scheduleActionMatches(desc.Schedule.Action, &cfg)
	if specMatches && actionMatches {
		fmt.Printf("  Schedule %q already configured (cron: %s)\n", cfg.ScheduleID, cfg.CronExpression)
		return nil
	}

	// Update the schedule with the new spec and action.
	// We replace the entire Spec rather than mutating fields because Temporal
	// parses CronExpressions into Calendars/StructuredCalendar server-side on
	// create. On subsequent describes, the cron lives in Calendars and
	// CronExpressions comes back empty — mutating CronExpressions alone would
	// leave stale calendars in place, causing the schedule to fire on both
	// the old and new cadences after every restart with a changed cron.
	//
	// We also rewrite the action's WorkflowInput so a newly-set
	// EmitterWebhookURL (or any other field) reaches scheduled runs
	// without manual schedule recreation.
	err = handle.Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			input.Description.Schedule.Spec = &client.ScheduleSpec{
				CronExpressions: []string{cfg.CronExpression},
				Jitter:          cfg.Jitter,
			}
			if action, ok := input.Description.Schedule.Action.(*client.ScheduleWorkflowAction); ok {
				action.TaskQueue = cfg.TaskQueue
				action.Args = []interface{}{orchestrator.WorkflowInput{
					ResourceTypes:     cfg.ResourceTypes,
					EmitterWebhookURL: cfg.EmitterWebhookURL,
					ScanScope:         orchestrator.ScanScopeFull,
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

	fmt.Printf("  Schedule %q updated (cron: %s)\n", cfg.ScheduleID, cfg.CronExpression)
	return nil
}

func isScheduleAlreadyRunning(err error) bool {
	return errors.Is(err, temporal.ErrScheduleAlreadyRunning)
}

// scheduleActionMatches reports whether the existing schedule's
// ScheduleWorkflowAction already carries the desired task queue and
// WorkflowInput fields. Anything we don't recognize (e.g. a non-workflow
// action, missing args) is treated as a mismatch so the update path
// rewrites it canonically.
func scheduleActionMatches(action client.ScheduleAction, cfg *Config) bool {
	wfAction, ok := action.(*client.ScheduleWorkflowAction)
	if !ok {
		return false
	}
	if wfAction.TaskQueue != cfg.TaskQueue {
		return false
	}
	if len(wfAction.Args) != 1 {
		return false
	}
	existing, ok := wfAction.Args[0].(orchestrator.WorkflowInput)
	if !ok {
		return false
	}
	if existing.EmitterWebhookURL != cfg.EmitterWebhookURL {
		return false
	}
	if existing.ScanScope != orchestrator.ScanScopeFull {
		return false
	}
	if !resourceTypesEqual(existing.ResourceTypes, cfg.ResourceTypes) {
		return false
	}
	return true
}

func resourceTypesEqual(a, b []types.ResourceType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
