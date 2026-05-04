package orchestrator

import (
	"context"
	"fmt"
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

//nolint:govet // field alignment sacrificed for logical grouping
type CreateSnapshotInput struct {
	ScanID        string
	ResourceTypes []types.ResourceType
	ScanStartTime time.Time
	ScanEndTime   time.Time

	// ExpectedFindingsCounts is the per-resource-type findings count that
	// the corresponding detection workflow reported. CreateSnapshot
	// compares it to what it actually finds in the store and refuses to
	// persist a snapshot if any expected count is missing.
	//
	// This guards against the "worker OOM mid-scan" failure mode where
	// detection workflows store findings in worker-local memory, the
	// worker dies, and a retry of CreateSnapshot on a fresh worker would
	// otherwise read an empty store and silently publish a 0-finding
	// snapshot to S3. With expected counts the activity fails (and
	// Temporal retries it) instead of persisting garbage.
	ExpectedFindingsCounts map[types.ResourceType]int
}

type SnapshotResult struct {
	SnapshotID           string
	TotalFindings        int
	CompliancePercentage float64
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
//
//nolint:gocritic // Temporal activity inputs are passed by value by convention; the SDK marshals them through its DataConverter
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
			return nil, fmt.Errorf("retrieve findings for %s: %w", resourceType, err)
		}
		logger.Info("Retrieved findings for snapshot", "resourceType", resourceType, "count", len(findings))

		// Refuse to persist a snapshot whose findings disagree with what
		// the detection workflow reported. This catches the case where
		// the worker that ran detection died (e.g. OOMKilled) and a
		// retry of CreateSnapshot lands on a fresh worker with an empty
		// in-memory store — without this check the activity would
		// happily publish a 0-finding snapshot to S3.
		if expected, ok := input.ExpectedFindingsCounts[resourceType]; ok && len(findings) != expected {
			return nil, fmt.Errorf(
				"findings count mismatch for %s: expected %d (from detection workflow), found %d in store; "+
					"the worker that ran detection likely restarted before the snapshot was persisted",
				resourceType, expected, len(findings),
			)
		}

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
