package orchestrator

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/block/Version-Guard/pkg/snapshot"
	"github.com/block/Version-Guard/pkg/store"
	"github.com/block/Version-Guard/pkg/telemetry"
	"github.com/block/Version-Guard/pkg/types"
)

// Activity names
const (
	CreateSnapshotActivityName           = "version-guard.CreateSnapshot"
	RecordResourceScanResultActivityName = "version-guard.RecordResourceScanResult"
)

// Activity input/output types

//nolint:govet // field alignment sacrificed for logical grouping
type CreateSnapshotInput struct {
	ScanID        string
	ResourceTypes []types.ResourceType
	ScanStartTime time.Time
	ScanEndTime   time.Time
}

type SnapshotResult struct {
	SnapshotID           string
	TotalFindings        int
	CompliancePercentage float64
}

type RecordResourceScanResultInput struct {
	ResourceType types.ResourceType
	Result       string
}

// Activities struct holds dependencies
type Activities struct {
	Store         store.Store
	SnapshotStore snapshot.Store
	// HTTPDoer is used by NotifyEmitter for the emitter webhook. Optional;
	// nil falls back to a default *http.Client with a 10s timeout. Tests
	// inject a fake to avoid real HTTP.
	HTTPDoer HTTPDoer
}

// NewActivities creates a new Activities instance
func NewActivities(
	store store.Store,
	snapshotStore snapshot.Store,
) *Activities {
	return &Activities{
		Store:         store,
		SnapshotStore: snapshotStore,
	}
}

// CreateSnapshot reads findings directly from the store and persists a snapshot to S3.
// This avoids passing large finding payloads through Temporal activity results,
// which would exceed the 4MB gRPC message limit for large inventories (12K+ resources).
//
//nolint:gocritic // Temporal activity inputs are passed by value by convention; the SDK marshals them through its DataConverter
func (a *Activities) CreateSnapshot(ctx context.Context, input CreateSnapshotInput) (*SnapshotResult, error) {
	result := telemetry.ResultFailure
	defer func() {
		telemetry.RecordSnapshotCreateAttempt(result)
	}()

	logger := activity.GetLogger(ctx)
	logger.Info("Creating snapshot", "scanID", input.ScanID, "resourceTypeCount", len(input.ResourceTypes))

	// Build snapshot by reading findings directly from the store per resource type
	builder := snapshot.NewBuilder()
	builder.WithScanTiming(input.ScanStartTime, input.ScanEndTime)

	for _, resourceType := range input.ResourceTypes {
		rt := resourceType
		findings, err := a.Store.ListFindings(ctx, store.FindingFilters{
			ResourceType: &rt,
		})
		if err != nil {
			return nil, fmt.Errorf("retrieve findings for %s: %w", resourceType, err)
		}
		logger.Info("Retrieved findings for snapshot", "resourceType", resourceType, "count", len(findings))
		builder.AddFindings(resourceType, findings)
	}

	snap := builder.Build()
	snap.SnapshotID = input.ScanID // Use scan ID as snapshot ID for correlation

	// Persist to S3
	err := a.SnapshotStore.SaveSnapshot(ctx, snap)
	if err != nil {
		return nil, err
	}

	logger.Info("Snapshot created and persisted",
		"snapshotID", snap.SnapshotID,
		"totalFindings", snap.Summary.TotalResources,
		"compliance", snap.Summary.CompliancePercentage)

	result = telemetry.ResultSuccess
	return &SnapshotResult{
		SnapshotID:           snap.SnapshotID,
		TotalFindings:        snap.Summary.TotalResources,
		CompliancePercentage: snap.Summary.CompliancePercentage,
	}, nil
}

// RecordResourceScanResult emits the logical result of a detection child
// workflow from an activity, keeping Prometheus side effects out of workflow
// replay code.
func (a *Activities) RecordResourceScanResult(ctx context.Context, input RecordResourceScanResultInput) error {
	telemetry.RecordDetectionRun(input.ResourceType, input.Result)
	activity.GetLogger(ctx).Info("Recorded resource scan result",
		"resourceType", input.ResourceType,
		"result", input.Result)
	return nil
}
