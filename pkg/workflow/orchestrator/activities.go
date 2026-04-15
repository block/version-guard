package orchestrator

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/block/Version-Guard/pkg/snapshot"
	"github.com/block/Version-Guard/pkg/store"
	"github.com/block/Version-Guard/pkg/types"
)

// Activity names
const (
	CreateSnapshotActivityName = "version-guard.CreateSnapshot"
)

// Activity input/output types

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

type SignalActWorkflowInput struct {
	SnapshotID string
}

// Activities struct holds dependencies
type Activities struct {
	Store         store.Store
	SnapshotStore snapshot.Store
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
func (a *Activities) CreateSnapshot(ctx context.Context, input CreateSnapshotInput) (*SnapshotResult, error) {
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
			logger.Warn("Failed to retrieve findings for snapshot", "resourceType", resourceType, "error", err)
			continue
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

	return &SnapshotResult{
		SnapshotID:           snap.SnapshotID,
		TotalFindings:        snap.Summary.TotalResources,
		CompliancePercentage: snap.Summary.CompliancePercentage,
	}, nil
}

// RegisterActivities registers all activities with a Temporal worker
func RegisterActivities(worker interface {
	RegisterActivityWithOptions(interface{}, activity.RegisterOptions)
}, activities *Activities) {
	worker.RegisterActivityWithOptions(activities.CreateSnapshot, activity.RegisterOptions{
		Name: CreateSnapshotActivityName,
	})
}
