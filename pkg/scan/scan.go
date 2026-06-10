// Package scan provides a reusable entry point for triggering a Version Guard
// scan (an OrchestratorWorkflow execution) from any caller (CLI, HTTP handler,
// etc.). It encapsulates workflow ID generation, input shaping, and Temporal
// client invocation so callers do not need to depend on Temporal internals.
package scan

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	"github.com/block/Version-Guard/pkg/telemetry"
	"github.com/block/Version-Guard/pkg/types"
	"github.com/block/Version-Guard/pkg/workflow/orchestrator"
)

// Default workflow execution timeout for a manually triggered scan.
// Matches the value used by the scheduled trigger in pkg/schedule.
const defaultExecutionTimeout = 2 * time.Hour

// Starter abstracts the subset of client.Client used to start a workflow,
// so callers can be tested without a real Temporal connection.
type Starter interface {
	ExecuteWorkflow(
		ctx context.Context,
		options client.StartWorkflowOptions,
		workflow interface{},
		args ...interface{},
	) (client.WorkflowRun, error)
}

// Trigger starts an OrchestratorWorkflow execution on demand.
type Trigger struct {
	starter              Starter
	taskQueue            string
	emitterWebhookURL    string
	defaultResourceTypes []types.ResourceType
}

// NewTrigger returns a Trigger backed by the given Temporal client.
// taskQueue must be the task queue the orchestrator worker is listening on.
// defaultResourceTypes is the list used when the caller does not specify
// any (e.g. a full-fleet scan via empty HTTP body); supply it from the
// loaded YAML config so adding a resource is a YAML-only change.
//
// Optional configuration (e.g. the emitter webhook URL) is set via
// functional options like WithEmitterWebhookURL so the constructor stays
// minimal and both real and test entry points share the same shape.
func NewTrigger(c client.Client, taskQueue string, defaultResourceTypes []types.ResourceType) *Trigger {
	return &Trigger{starter: c, taskQueue: taskQueue, defaultResourceTypes: defaultResourceTypes}
}

// NewTriggerWithStarter returns a Trigger backed by an explicit Starter
// (used for testing). defaultResourceTypes may be nil if every test path
// supplies an explicit list.
func NewTriggerWithStarter(s Starter, taskQueue string, defaultResourceTypes []types.ResourceType) *Trigger {
	return &Trigger{starter: s, taskQueue: taskQueue, defaultResourceTypes: defaultResourceTypes}
}

// WithEmitterWebhookURL returns a copy of the trigger configured to forward
// the given URL to every started OrchestratorWorkflow. The notify
// activity in the orchestrator is gated on this field being non-empty.
func (t *Trigger) WithEmitterWebhookURL(url string) *Trigger {
	clone := *t
	clone.emitterWebhookURL = url
	return &clone
}

// Input controls the scope of a manual scan.
type Input struct {
	// ScanID lets the caller pin a correlation ID. If empty, one is generated.
	ScanID string

	// Source identifies the trigger transport, e.g. "http" or "cli".
	// Empty is recorded as "manual".
	Source string

	// ResourceTypes limits the scan to the given resource config IDs
	// (e.g. "aurora-mysql", "eks"). Empty means scan all configured resources.
	ResourceTypes []types.ResourceType
}

// Result describes a started scan.
type Result struct {
	WorkflowID string
	RunID      string
	ScanID     string
}

// Run starts an OrchestratorWorkflow and returns identifiers describing the
// running execution. It does not wait for completion.
func (t *Trigger) Run(ctx context.Context, in Input) (res Result, err error) {
	start := time.Now()
	source := telemetry.NormalizeScanSource(in.Source)
	result := telemetry.ResultFailure
	scanID := in.ScanID
	workflowID := ""
	runID := ""
	resourceTypeCount := len(in.ResourceTypes)
	defer func() {
		telemetry.RecordScanTrigger(source, result, t.taskQueue, time.Since(start))
		attrs := []any{
			"source", source,
			"scanID", scanID,
			"workflowID", workflowID,
			"runID", runID,
			"taskQueue", t.taskQueue,
			"resourceTypeCount", resourceTypeCount,
		}
		if err != nil {
			attrs = append(attrs, "event", "scan_trigger_failed", "error", err)
			slog.Error("scan trigger failed", attrs...)
			return
		}
		attrs = append(attrs, "event", "scan_triggered")
		slog.Info("scan triggered", attrs...)
	}()

	if t.taskQueue == "" {
		return Result{}, fmt.Errorf("scan: task queue is required")
	}

	if scanID == "" {
		scanID = uuid.NewString()
	}

	// Empty caller list means "full fleet scan" — fall back to the
	// configured default. The orchestrator workflow rejects empty
	// ResourceTypes (see orchestrator.ErrNoResourceTypes), so this is
	// the contract boundary that translates "no body / full scan"
	// into the YAML-derived list.
	resourceTypes := in.ResourceTypes
	scanScope := orchestrator.ScanScopeTargeted
	if len(resourceTypes) == 0 {
		resourceTypes = t.defaultResourceTypes
		scanScope = orchestrator.ScanScopeFull
	}
	resourceTypeCount = len(resourceTypes)
	if len(resourceTypes) == 0 {
		return Result{}, fmt.Errorf("scan: no resource types to scan and no default configured")
	}

	workflowID = buildWorkflowID(scanID)

	opts := client.StartWorkflowOptions{
		ID:                                       workflowID,
		TaskQueue:                                t.taskQueue,
		WorkflowExecutionTimeout:                 defaultExecutionTimeout,
		WorkflowIDConflictPolicy:                 enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
		WorkflowExecutionErrorWhenAlreadyStarted: true,
	}

	run, err := t.starter.ExecuteWorkflow(ctx, opts, orchestrator.OrchestratorWorkflow, orchestrator.WorkflowInput{
		ScanID:            scanID,
		ResourceTypes:     resourceTypes,
		EmitterWebhookURL: t.emitterWebhookURL,
		ScanScope:         scanScope,
	})
	if err != nil {
		return Result{}, fmt.Errorf("scan: execute workflow: %w", err)
	}

	runID = run.GetRunID()
	result = telemetry.ResultSuccess
	return Result{
		WorkflowID: run.GetID(),
		RunID:      runID,
		ScanID:     scanID,
	}, nil
}

// buildWorkflowID returns the singleton orchestrator workflow ID. Temporal
// rejects a new run with this ID while the previous scan is still open, which
// keeps the worker-local findings store safe to use as per-scan scratch space.
func buildWorkflowID(_ string) string {
	return orchestrator.ActiveScanWorkflowID
}
