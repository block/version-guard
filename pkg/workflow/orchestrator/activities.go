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
	ScanScope     string
	ResourceTypes []types.ResourceType
	// ExpectedResourceTypes is populated for full scans and represents the
	// configured resource families that must appear in the persisted snapshot.
	ExpectedResourceTypes []types.ResourceType
	ScanStartTime         time.Time
	ScanEndTime           time.Time
}

type SnapshotResult struct {
	SnapshotID           string
	TotalFindings        int
	CompliancePercentage float64
}

type RecordResourceScanResultInput struct {
	ResourceType   types.ResourceType
	Result         string
	DurationMillis int64
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
	scanScope := normalizeScanScope(input.ScanScope)
	logger.Info("Creating snapshot",
		"scanID", input.ScanID,
		"scanScope", scanScope,
		"resourceTypeCount", len(input.ResourceTypes),
		"expectedResourceTypeCount", len(input.ExpectedResourceTypes))

	// Build snapshot by reading findings directly from the store per resource type
	builder := snapshot.NewBuilder()
	builder.WithScanTiming(input.ScanStartTime, input.ScanEndTime)

	for _, resourceType := range input.ResourceTypes {
		rt := resourceType
		findings, err := a.Store.ListFindings(ctx, store.FindingFilters{
			ResourceType: &rt,
		})
		if err != nil {
			telemetry.RecordSnapshotValidation(telemetry.ResultFailure, telemetry.SnapshotValidationReasonStoreReadFailed)
			return nil, fmt.Errorf("retrieve findings for %s: %w", resourceType, err)
		}
		logger.Info("Retrieved findings for snapshot", "resourceType", resourceType, "count", len(findings))
		builder.AddFindings(resourceType, findings)
	}

	snap := builder.Build()
	snap.SnapshotID = input.ScanID // Use scan ID as snapshot ID for correlation

	validationReason, validationErr := validateSnapshotCompleteness(&input, snap)
	if validationErr != nil {
		telemetry.RecordSnapshotValidation(telemetry.ResultFailure, validationReason)
		logger.Error("Snapshot validation failed",
			"scanID", input.ScanID,
			"scanScope", scanScope,
			"reason", validationReason,
			"error", validationErr)
		return nil, validationErr
	}
	telemetry.RecordSnapshotValidation(telemetry.ResultSuccess, telemetry.SnapshotValidationReasonOK)

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
	telemetry.RecordSnapshotLastValid(scanScope)
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
	telemetry.RecordDetectionRunWithDuration(input.ResourceType, input.Result, time.Duration(input.DurationMillis)*time.Millisecond)
	activity.GetLogger(ctx).Info("Recorded resource scan result",
		"resourceType", input.ResourceType,
		"result", input.Result)
	return nil
}

func validateSnapshotCompleteness(input *CreateSnapshotInput, snap *types.Snapshot) (string, error) {
	if !shouldValidateSnapshotCompleteness(input) {
		return telemetry.SnapshotValidationReasonOK, nil
	}

	presentResourceTypes := snapshotResourceTypes(snap)
	telemetry.RecordSnapshotResourceTypes(input.ExpectedResourceTypes, presentResourceTypes)

	if len(input.ExpectedResourceTypes) == 0 {
		return telemetry.SnapshotValidationReasonEmptyExpectedSet, fmt.Errorf("snapshot validation failed: expected resource type set is empty")
	}
	if snap == nil || len(presentResourceTypes) == 0 {
		return telemetry.SnapshotValidationReasonEmptySnapshot, fmt.Errorf("snapshot validation failed: snapshot contains no resource types")
	}
	if err := validateSnapshotSummary(snap); err != nil {
		return telemetry.SnapshotValidationReasonInvalidSummary, fmt.Errorf("snapshot validation failed: %w", err)
	}

	missing := missingResourceTypes(input.ExpectedResourceTypes, presentResourceTypes)
	if len(missing) > 0 {
		return telemetry.SnapshotValidationReasonMissingResourceType, fmt.Errorf("snapshot validation failed: missing expected resource types: %v", missing)
	}

	return telemetry.SnapshotValidationReasonOK, nil
}

func shouldValidateSnapshotCompleteness(input *CreateSnapshotInput) bool {
	if input == nil {
		return false
	}
	if normalizeScanScope(input.ScanScope) != ScanScopeFull {
		return false
	}
	// Pre-validation workflow histories did not carry scan scope or expected
	// resource types. Keep those replay paths on their original behavior.
	return input.ScanScope != "" || len(input.ExpectedResourceTypes) > 0
}

func snapshotResourceTypes(snap *types.Snapshot) []types.ResourceType {
	if snap == nil {
		return nil
	}

	resourceTypes := make([]types.ResourceType, 0, len(snap.FindingsByType))
	for resourceType := range snap.FindingsByType {
		resourceTypes = append(resourceTypes, resourceType)
	}
	return resourceTypes
}

func missingResourceTypes(expected, present []types.ResourceType) []types.ResourceType {
	presentSet := make(map[types.ResourceType]struct{}, len(present))
	for _, resourceType := range present {
		presentSet[resourceType] = struct{}{}
	}

	missing := make([]types.ResourceType, 0)
	for _, resourceType := range expected {
		if _, ok := presentSet[resourceType]; !ok {
			missing = append(missing, resourceType)
		}
	}
	return missing
}

func validateSnapshotSummary(snap *types.Snapshot) error {
	if snap.Summary.ByResourceType == nil {
		return fmt.Errorf("summary by_resource_type is nil")
	}

	totalByResourceType := 0
	for resourceType, findings := range snap.FindingsByType {
		bucket, ok := snap.Summary.ByResourceType[resourceType]
		if !ok || bucket == nil {
			return fmt.Errorf("summary missing resource type %s", resourceType)
		}
		if bucket.TotalResources != len(findings) {
			return fmt.Errorf("summary total for %s is %d, findings contain %d", resourceType, bucket.TotalResources, len(findings))
		}
		totalByResourceType += bucket.TotalResources
	}
	if totalByResourceType != snap.Summary.TotalResources {
		return fmt.Errorf("summary total is %d, by_resource_type totals sum to %d", snap.Summary.TotalResources, totalByResourceType)
	}

	return nil
}
