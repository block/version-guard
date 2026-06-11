package orchestrator

import (
	"fmt"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/block/Version-Guard/pkg/types"
	detectionWorkflow "github.com/block/Version-Guard/pkg/workflow/detection"
)

// ErrNoResourceTypes is returned when the orchestrator is invoked without
// any resource types to scan. Callers (the HTTP scan trigger and the
// scheduled trigger in pkg/schedule) are responsible for sourcing the
// default list from the loaded YAML config and passing it on the input
// — keeping the workflow free of hardcoded resource-type strings is what
// makes adding a new resource a YAML-only change.
var ErrNoResourceTypes = fmt.Errorf("orchestrator: WorkflowInput.ResourceTypes is empty; the caller must populate it from configured resources")

// Workflow constants
const (
	OrchestratorWorkflowType = "VersionGuardOrchestratorWorkflow"
	TaskQueueName            = "version-guard-orchestrator"
	ActiveScanWorkflowID     = "version-guard-active-scan"
	ScheduledScanWorkflowID  = "version-guard-scheduled-scan"

	ScanScopeFull     = "full"
	ScanScopeTargeted = "targeted"
)

// WorkflowInput defines the input for the orchestrator workflow.
// Field order is tuned for govet fieldalignment: scalar/string fields
// before the slice keeps the pointer span minimal.
type WorkflowInput struct {
	ScanID string
	// EmitterWebhookURL, when set, makes the orchestrator POST to
	// "<url>/trigger-act" after the snapshot is persisted (emitter webhook).
	// Empty disables the webhook — the snapshot remains durable in S3
	// and downstream emitters can pull on their own cadence.
	EmitterWebhookURL string
	// ScanScope identifies whether the scan should be validated as a full
	// configured-resource scan or treated as an intentionally targeted run.
	// Empty values from pre-scope callers are treated as full scans once
	// snapshot validation is enabled for the workflow history.
	ScanScope     string
	ResourceTypes []types.ResourceType // If empty, scan all supported types
}

// WorkflowOutput contains the results of the orchestrator workflow
//
//nolint:govet // field alignment sacrificed for logical grouping
type WorkflowOutput struct {
	ScanID               string
	SnapshotID           string
	TotalFindings        int
	CompliancePercentage float64
	ResourceTypeResults  map[types.ResourceType]*ResourceTypeResult
	StartTime            time.Time
	EndTime              time.Time
	DurationSec          int64
}

// ResourceTypeResult contains the result for a single resource type scan
//
//nolint:govet // field alignment sacrificed for logical grouping
type ResourceTypeResult struct {
	ResourceType   types.ResourceType
	FindingsCount  int
	RedCount       int
	YellowCount    int
	GreenCount     int
	UnknownCount   int
	DurationMillis int64
	Error          string // Empty if successful
}

// ScheduledScanWorkflow is the Temporal schedule entry point. Temporal appends
// the scheduled fire time to workflow IDs for uniqueness, so schedules cannot
// start OrchestratorWorkflow directly while it enforces ActiveScanWorkflowID.
// This launcher may have a timestamp-suffixed ID, but it starts the real scan
// as a child workflow using the fixed singleton ID. That keeps scheduled and
// manual scans mutually exclusive while preserving unique scheduled ScanIDs.
func ScheduledScanWorkflow(ctx workflow.Context, input WorkflowInput) (*WorkflowOutput, error) {
	info := workflow.GetInfo(ctx)
	if input.ScanID == "" {
		input.ScanID = info.WorkflowExecution.ID
	}

	childOpts := workflow.ChildWorkflowOptions{
		WorkflowID:               ActiveScanWorkflowID,
		WorkflowExecutionTimeout: 2 * time.Hour,
		WorkflowIDReusePolicy:    enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}
	childCtx := workflow.WithChildOptions(ctx, childOpts)

	var output WorkflowOutput
	err := workflow.ExecuteChildWorkflow(childCtx, OrchestratorWorkflow, input).Get(ctx, &output)
	if err != nil {
		logger := workflow.GetLogger(ctx)
		logger.Error("Scheduled scan workflow failed",
			"event", "scheduled_scan_workflow_failed",
			"scanID", input.ScanID,
			"workflowID", info.WorkflowExecution.ID,
			"runID", info.WorkflowExecution.RunID,
			"childWorkflowID", ActiveScanWorkflowID,
			"error", err)
		return nil, err
	}

	return &output, nil
}

// OrchestratorWorkflow is the main workflow that orchestrates the three-stage pipeline:
// Stage 1: Detect - Fan out across resource types in parallel
// Stage 2: Store - Write classified findings to S3 as versioned snapshot
//
// Name note: revive flags this as a stutter (orchestrator.OrchestratorWorkflow),
// but Temporal's RegisterWorkflow derives the registered workflow type from the
// Go function name. Renaming would change the on-the-wire workflow type and
// invalidate any persisted workflow histories.
//
//nolint:revive // see comment above; rename would be a Temporal wire-format break
func OrchestratorWorkflow(ctx workflow.Context, input WorkflowInput) (*WorkflowOutput, error) {
	logger := workflow.GetLogger(ctx)
	info := workflow.GetInfo(ctx)
	if info.WorkflowExecution.ID != ActiveScanWorkflowID {
		err := fmt.Errorf("orchestrator: workflow ID must be %q, got %q", ActiveScanWorkflowID, info.WorkflowExecution.ID)
		logger.Error("Orchestrator workflow failed",
			"event", "scan_workflow_failed",
			"workflowID", info.WorkflowExecution.ID,
			"runID", info.WorkflowExecution.RunID,
			"error", err)
		return nil, err
	}

	// Ensure ScanID is set for correlation across child workflows and snapshots
	// (scheduled executions pass empty ScanID)
	if input.ScanID == "" {
		input.ScanID = info.WorkflowExecution.ID
	}
	scanScope := normalizeScanScope(input.ScanScope)

	logger.Info("Starting orchestrator workflow",
		"event", "scan_workflow_started",
		"scanID", input.ScanID,
		"scanScope", scanScope,
		"workflowID", info.WorkflowExecution.ID,
		"runID", info.WorkflowExecution.RunID)

	startTime := workflow.Now(ctx)

	// The list of resource types to scan must be supplied by the caller
	// (HTTP scan trigger or the scheduled trigger), sourced from the
	// loaded YAML config. The orchestrator deliberately does NOT carry
	// a hardcoded fallback list — that would silently re-introduce the
	// "adding a resource requires a Go change" coupling we removed.
	resourceTypes := input.ResourceTypes
	if len(resourceTypes) == 0 {
		logger.Error("Orchestrator workflow failed",
			"event", "scan_workflow_failed",
			"scanID", input.ScanID,
			"workflowID", info.WorkflowExecution.ID,
			"runID", info.WorkflowExecution.RunID,
			"error", ErrNoResourceTypes)
		return nil, ErrNoResourceTypes
	}

	// Retry policy for child workflows
	retryPolicy := &temporal.RetryPolicy{
		InitialInterval:    time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    100 * time.Second,
		MaximumAttempts:    3,
	}

	// Child workflow options
	childWorkflowOptions := workflow.ChildWorkflowOptions{
		WorkflowExecutionTimeout: 60 * time.Minute,
		WorkflowTaskTimeout:      time.Minute,
		RetryPolicy:              retryPolicy,
	}

	// Stage 1: DETECT - Fan out across resource types in parallel
	logger.Info("Stage 1: Detect - Starting parallel detection workflows", "resourceTypeCount", len(resourceTypes))

	// Launch child workflows in parallel
	futures := make(map[types.ResourceType]workflow.ChildWorkflowFuture)
	for _, resourceType := range resourceTypes {
		childCtx := workflow.WithChildOptions(ctx, childWorkflowOptions)

		childInput := detectionWorkflow.WorkflowInput{
			ScanID:       input.ScanID,
			ResourceType: resourceType,
		}

		future := workflow.ExecuteChildWorkflow(childCtx, detectionWorkflow.DetectionWorkflow, childInput)
		futures[resourceType] = future
	}

	// Wait for all child workflows to complete and collect results.
	//
	// We iterate the input `resourceTypes` slice rather than ranging
	// over the `futures` map. Map iteration order is unstable in Go,
	// and `successfulTypes` becomes part of CreateSnapshotInput below
	// — Temporal records activity inputs in workflow history. A
	// different ordering on replay than the original execution would
	// produce a different activity input hash and (depending on SDK
	// version) either a non-determinism panic or a silently
	// differently-ordered snapshot. Iterating the slice keeps the
	// order pinned to the input order, which the caller controls.
	resourceTypeResults := make(map[types.ResourceType]*ResourceTypeResult, len(resourceTypes))
	successfulTypes := make([]types.ResourceType, 0, len(resourceTypes))
	recordResourceScanResults := workflow.GetVersion(ctx, "record-resource-scan-result", workflow.DefaultVersion, 1) == 1
	metricsActivityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    10 * time.Second,
			MaximumAttempts:    3,
		},
	})

	for _, resourceType := range resourceTypes {
		future := futures[resourceType]
		var output detectionWorkflow.WorkflowOutput
		err := future.Get(ctx, &output)

		result := &ResourceTypeResult{
			ResourceType:   resourceType,
			DurationMillis: output.DurationMillis,
		}

		if err != nil {
			logger.Error("Child workflow failed",
				"event", "scan_resource_workflow_failed",
				"scanID", input.ScanID,
				"resourceType", resourceType,
				"error", err)
			if recordResourceScanResults {
				recordErr := workflow.ExecuteActivity(
					metricsActivityCtx,
					RecordResourceScanResultActivityName,
					RecordResourceScanResultInput{
						ResourceType:   resourceType,
						Result:         "failure",
						DurationMillis: result.DurationMillis,
					},
				).Get(metricsActivityCtx, nil)
				if recordErr != nil {
					logger.Warn("Failed to record resource scan result",
						"scanID", input.ScanID,
						"resourceType", resourceType,
						"result", "failure",
						"error", recordErr)
				}
			}
			result.Error = err.Error()
			resourceTypeResults[resourceType] = result
			continue
		}

		// Populate result with summary data
		result.FindingsCount = output.FindingsCount
		if output.Summary != nil {
			result.RedCount = output.Summary.RedCount
			result.YellowCount = output.Summary.YellowCount
			result.GreenCount = output.Summary.GreenCount
			result.UnknownCount = output.Summary.UnknownCount
		}

		resourceTypeResults[resourceType] = result
		successfulTypes = append(successfulTypes, resourceType)
		if recordResourceScanResults {
			recordErr := workflow.ExecuteActivity(
				metricsActivityCtx,
				RecordResourceScanResultActivityName,
				RecordResourceScanResultInput{
					ResourceType:   resourceType,
					Result:         "success",
					DurationMillis: result.DurationMillis,
				},
			).Get(metricsActivityCtx, nil)
			if recordErr != nil {
				logger.Warn("Failed to record resource scan result",
					"scanID", input.ScanID,
					"resourceType", resourceType,
					"result", "success",
					"error", recordErr)
			}
		}
	}

	logger.Info("Stage 1: Detect - All detection workflows completed", "successCount", len(successfulTypes))

	if len(successfulTypes) == 0 {
		err := fmt.Errorf("all detection workflows failed; no findings to snapshot")
		logger.Error("Orchestrator workflow failed",
			"event", "scan_workflow_failed",
			"scanID", input.ScanID,
			"workflowID", info.WorkflowExecution.ID,
			"runID", info.WorkflowExecution.RunID,
			"error", err)
		return nil, err
	}

	// Stage 2: STORE - Create and persist snapshot to S3
	logger.Info("Stage 2: Store - Creating snapshot")

	var snapshotResult SnapshotResult
	snapshotInput := newCreateSnapshotInput(ctx, input.ScanID, scanScope, resourceTypes, successfulTypes, startTime)
	err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Minute,
			RetryPolicy:         retryPolicy,
		}),
		CreateSnapshotActivityName,
		snapshotInput,
	).Get(ctx, &snapshotResult)

	if err != nil {
		logger.Error("Failed to create snapshot",
			"event", "scan_workflow_failed",
			"scanID", input.ScanID,
			"workflowID", info.WorkflowExecution.ID,
			"runID", info.WorkflowExecution.RunID,
			"error", err)
		return nil, err
	}

	logger.Info("Stage 2: Store - Snapshot created and persisted", "snapshotID", snapshotResult.SnapshotID)

	// NOTIFY EMITTER (optional out-of-process webhook)
	//
	// When EmitterWebhookURL is configured, POST the snapshot id to the
	// downstream emitter so it can start its own workflow against the
	// freshly-persisted snapshot. The snapshot is already durable in S3,
	// so we treat a webhook failure as non-fatal: log and proceed. Other
	// implementers can subscribe to S3 events, poll, or run a schedule
	// instead — the webhook is one supported integration, not the only one.
	if input.EmitterWebhookURL != "" {
		logger.Info("Notify - Calling emitter webhook",
			"url", input.EmitterWebhookURL,
			"snapshotID", snapshotResult.SnapshotID)

		var notifyResult NotifyEmitterResult
		notifyErr := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
				StartToCloseTimeout: 30 * time.Second,
				RetryPolicy: &temporal.RetryPolicy{
					InitialInterval:    time.Second,
					BackoffCoefficient: 2.0,
					MaximumInterval:    10 * time.Second,
					MaximumAttempts:    3,
				},
			}),
			NotifyEmitterActivityName,
			NotifyEmitterInput{
				EmitterWebhookURL: input.EmitterWebhookURL,
				SnapshotID:        snapshotResult.SnapshotID,
			},
		).Get(ctx, &notifyResult)

		if notifyErr != nil {
			logger.Warn("Notify - Emitter webhook failed; snapshot remains in S3 for later pickup",
				"error", notifyErr,
				"snapshotID", snapshotResult.SnapshotID)
		} else {
			logger.Info("Notify - Emitter accepted snapshot",
				"snapshotID", snapshotResult.SnapshotID,
				"emitterWorkflowID", notifyResult.WorkflowID,
				"emitterRunID", notifyResult.RunID)
		}
	} else {
		logger.Info("Notify - Skipped (no EmitterWebhookURL configured); snapshot available in S3",
			"snapshotID", snapshotResult.SnapshotID)
	}

	endTime := workflow.Now(ctx)

	output := &WorkflowOutput{
		ScanID:               input.ScanID,
		SnapshotID:           snapshotResult.SnapshotID,
		TotalFindings:        snapshotResult.TotalFindings,
		CompliancePercentage: snapshotResult.CompliancePercentage,
		ResourceTypeResults:  resourceTypeResults,
		StartTime:            startTime,
		EndTime:              endTime,
		DurationSec:          int64(endTime.Sub(startTime).Seconds()),
	}

	logger.Info("Orchestrator workflow completed",
		"event", "scan_workflow_completed",
		"scanID", output.ScanID,
		"workflowID", info.WorkflowExecution.ID,
		"runID", info.WorkflowExecution.RunID,
		"snapshotID", output.SnapshotID,
		"totalFindings", output.TotalFindings,
		"compliance", output.CompliancePercentage,
		"durationSec", output.DurationSec)

	return output, nil
}

func normalizeScanScope(scanScope string) string {
	switch scanScope {
	case ScanScopeTargeted:
		return ScanScopeTargeted
	default:
		return ScanScopeFull
	}
}

func newCreateSnapshotInput(
	ctx workflow.Context,
	scanID string,
	scanScope string,
	expectedResourceTypes []types.ResourceType,
	successfulResourceTypes []types.ResourceType,
	startTime time.Time,
) CreateSnapshotInput {
	input := CreateSnapshotInput{
		ScanID:        scanID,
		ResourceTypes: successfulResourceTypes,
		ScanStartTime: startTime,
		ScanEndTime:   workflow.Now(ctx),
	}
	if workflow.GetVersion(ctx, "snapshot-completeness-validation", workflow.DefaultVersion, 1) == 1 {
		input.ScanScope = scanScope
		input.ExpectedResourceTypes = expectedResourceTypes
	}
	return input
}
