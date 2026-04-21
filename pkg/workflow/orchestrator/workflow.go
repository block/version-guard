package orchestrator

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/block/Version-Guard/pkg/types"
	detectionWorkflow "github.com/block/Version-Guard/pkg/workflow/detection"
)

// Workflow constants
const (
	OrchestratorWorkflowType = "VersionGuardOrchestratorWorkflow"
	TaskQueueName            = "version-guard-orchestrator"
)

// WorkflowInput defines the input for the orchestrator workflow
type WorkflowInput struct {
	ScanID        string
	ResourceTypes []types.ResourceType // If empty, scan all supported types
}

// WorkflowOutput contains the results of the orchestrator workflow
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

// OrchestratorWorkflow is the main workflow that orchestrates the three-stage pipeline:
// Stage 1: Detect - Fan out across resource types in parallel
// Stage 2: Store - Write classified findings to S3 as versioned snapshot
func OrchestratorWorkflow(ctx workflow.Context, input WorkflowInput) (*WorkflowOutput, error) {
	logger := workflow.GetLogger(ctx)

	// Ensure ScanID is set for correlation across child workflows and snapshots
	// (scheduled executions pass empty ScanID)
	if input.ScanID == "" {
		input.ScanID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}

	logger.Info("Starting orchestrator workflow", "scanID", input.ScanID)

	startTime := workflow.Now(ctx)

	// Default to all supported resource types if none specified
	resourceTypes := input.ResourceTypes
	if len(resourceTypes) == 0 {
		// Use config IDs from resources.yaml (not resource types)
		// to support multiple resources of the same type
		resourceTypes = []types.ResourceType{
			"aurora-postgresql",
			"aurora-mysql",
			"eks",
			"elasticache-redis",
			"opensearch",
			"rds-mysql",
			"rds-postgresql",
			"lambda",
		}
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

	// Wait for all child workflows to complete and collect results
	resourceTypeResults := make(map[types.ResourceType]*ResourceTypeResult)
	var successfulTypes []types.ResourceType

	for resourceType, future := range futures {
		var output detectionWorkflow.WorkflowOutput
		err := future.Get(ctx, &output)

		result := &ResourceTypeResult{
			ResourceType:   resourceType,
			DurationMillis: output.DurationMillis,
		}

		if err != nil {
			logger.Error("Child workflow failed", "resourceType", resourceType, "error", err)
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
	}

	logger.Info("Stage 1: Detect - All detection workflows completed", "successCount", len(successfulTypes))

	if len(successfulTypes) == 0 {
		return nil, fmt.Errorf("all detection workflows failed; no findings to snapshot")
	}

	// Stage 2: STORE - Create and persist snapshot to S3
	logger.Info("Stage 2: Store - Creating snapshot")

	var snapshotResult SnapshotResult
	err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Minute,
			RetryPolicy:         retryPolicy,
		}),
		CreateSnapshotActivityName,
		CreateSnapshotInput{
			ScanID:        input.ScanID,
			ResourceTypes: successfulTypes,
			ScanStartTime: startTime,
			ScanEndTime:   workflow.Now(ctx),
		},
	).Get(ctx, &snapshotResult)

	if err != nil {
		logger.Error("Failed to create snapshot", "error", err)
		return nil, err
	}

	logger.Info("Stage 2: Store - Snapshot created and persisted", "snapshotID", snapshotResult.SnapshotID)

	// Stage 3: Emit - Implementers should create their own workflow or process
	// to consume the snapshot from S3 and emit findings to their chosen destinations.
	// See pkg/emitters/emitters.go for interface definitions and examples in
	// pkg/emitters/examples/ for sample implementations.
	logger.Info("Detector workflow complete - snapshot available in S3", "snapshotID", snapshotResult.SnapshotID)

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
		"snapshotID", output.SnapshotID,
		"totalFindings", output.TotalFindings,
		"compliance", output.CompliancePercentage,
		"durationSec", output.DurationSec)

	return output, nil
}
