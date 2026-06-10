package detection

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/block/Version-Guard/pkg/types"
)

// Workflow constants (stable for replay)
const (
	DetectionWorkflowType = "VersionGuardDetectionWorkflow"
	TaskQueueName         = "version-guard-detection"
)

// WorkflowInput defines the input parameters for the detection workflow
type WorkflowInput struct {
	ScanID       string
	ResourceType types.ResourceType
}

// WorkflowOutput contains the results of the detection workflow
//
//nolint:govet // field alignment sacrificed for logical grouping
type WorkflowOutput struct {
	ScanID         string
	ResourceType   types.ResourceType
	FindingsCount  int
	Summary        *types.ScanSummary
	CompletedAt    time.Time
	DurationMillis int64
}

// DetectionWorkflow is the main Temporal workflow for version drift detection
// It orchestrates: inventory fetch → EOL lookup → detection → storage → metrics
//
// This workflow uses Temporal Sessions to pin all activities to the same worker.
// Activities share large data (resources, findings) via in-memory caches on the
// Activities struct rather than serializing them into Temporal payloads, which
// would exceed the 4MB gRPC message limit for real inventory data (~10MB for
// 12K+ Aurora clusters). Sessions guarantee all activities in this workflow
// execute on the same worker process, making the in-memory cache safe.
//
//nolint:revive // DetectionWorkflow is the conventional Temporal workflow naming
func DetectionWorkflow(ctx workflow.Context, input WorkflowInput) (*WorkflowOutput, error) {
	logger := workflow.GetLogger(ctx)

	// Ensure ScanID is set for resource caching between activities
	if input.ScanID == "" {
		input.ScanID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}

	logger.Info("Starting detection workflow", "scanID", input.ScanID, "resourceType", input.ResourceType)

	startTime := workflow.Now(ctx)

	// Create a session to pin all activities to the same worker. This is required
	// because FetchInventory caches resources in worker-local memory (sync.Map),
	// and downstream activities (FetchEOLData, DetectDrift, etc.) read from that
	// cache. Without a session, Temporal could schedule activities on different
	// workers, where the cache would be empty.
	sessionCtx, err := workflow.CreateSession(ctx, &workflow.SessionOptions{
		CreationTimeout:  time.Minute,
		ExecutionTimeout: 35 * time.Minute, // enough for the full pipeline
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer workflow.CompleteSession(sessionCtx)

	// Retry policy for activities (exponential backoff)
	retryPolicy := &temporal.RetryPolicy{
		InitialInterval:    time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    100 * time.Second,
		MaximumAttempts:    5,
	}

	// Short context: 2 min (metadata, fast operations)
	// Activity options are layered on top of the session context so that
	// the session's worker-pinning is preserved.
	shortCtx := workflow.WithActivityOptions(sessionCtx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy:         retryPolicy,
	})

	// Long context: 30 min (API calls, inventory fetch)
	longCtx := workflow.WithActivityOptions(sessionCtx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy:         retryPolicy,
	})

	// Activity 1: Fetch inventory (LONG)
	var inventoryResult InventoryResult
	err = workflow.ExecuteActivity(longCtx, FetchInventoryActivityName, FetchInventoryInput(input)).Get(longCtx, &inventoryResult)
	if err != nil {
		logger.Error("Failed to fetch inventory", "error", err)
		return nil, err
	}

	logger.Info("Inventory fetched", "resourceBatchID", inventoryResult.ResourceBatchID, "resourceCount", len(inventoryResult.Resources))

	// Activity 2: Fetch EOL data (LONG)
	var eolResult EOLResult
	err = workflow.ExecuteActivity(longCtx, FetchEOLDataActivityName, FetchEOLInput{
		ResourceType:    input.ResourceType,
		ResourceBatchID: inventoryResult.ResourceBatchID,
		Resources:       inventoryResult.Resources,
	}).Get(longCtx, &eolResult)
	if err != nil {
		logger.Error("Failed to fetch EOL data", "error", err)
		return nil, err
	}

	logger.Info("EOL data fetched", "versionCount", len(eolResult.VersionLifecycles))

	// Activity 3: Detect drift (SHORT)
	var detectResult DetectResult
	err = workflow.ExecuteActivity(shortCtx, DetectDriftActivityName, DetectInput{
		ResourceBatchID:   inventoryResult.ResourceBatchID,
		Resources:         inventoryResult.Resources,
		VersionLifecycles: eolResult.VersionLifecycles,
	}).Get(shortCtx, &detectResult)
	if err != nil {
		logger.Error("Failed to detect drift", "error", err)
		return nil, err
	}

	logger.Info("Drift detected", "findingsCount", detectResult.FindingsCount)

	// Activity 4: Store findings (SHORT)
	err = workflow.ExecuteActivity(shortCtx, StoreFindingsActivityName, StoreInput{
		FindingsBatchID: detectResult.FindingsBatchID,
		Findings:        detectResult.Findings,
		ResourceType:    input.ResourceType,
	}).Get(shortCtx, nil)
	if err != nil {
		logger.Error("Failed to store findings", "error", err)
		return nil, err
	}

	logger.Info("Findings stored")

	// Activity 5: Emit metrics (SHORT)
	var metricsResult MetricsResult
	err = workflow.ExecuteActivity(shortCtx, EmitMetricsActivityName, MetricsInput{
		FindingsBatchID: detectResult.FindingsBatchID,
		Findings:        detectResult.Findings,
		ResourceType:    input.ResourceType,
	}).Get(shortCtx, &metricsResult)
	if err != nil {
		logger.Error("Failed to emit metrics", "error", err)
		// Don't fail workflow on metrics errors
		logger.Warn("Continuing despite metrics error")
	}

	// Calculate duration
	endTime := workflow.Now(ctx)
	duration := endTime.Sub(startTime)

	output := &WorkflowOutput{
		ScanID:         input.ScanID,
		ResourceType:   input.ResourceType,
		FindingsCount:  detectResult.FindingsCount,
		Summary:        metricsResult.Summary,
		CompletedAt:    endTime,
		DurationMillis: duration.Milliseconds(),
	}

	logger.Info("Detection workflow completed",
		"scanID", input.ScanID,
		"findings", output.FindingsCount,
		"durationMs", output.DurationMillis)

	return output, nil
}
