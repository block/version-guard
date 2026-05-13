package detection

import (
	"context"
	"fmt"
	"sync"

	"go.temporal.io/sdk/activity"

	"github.com/block/Version-Guard/pkg/eol"
	"github.com/block/Version-Guard/pkg/inventory"
	"github.com/block/Version-Guard/pkg/policy"
	"github.com/block/Version-Guard/pkg/store"
	"github.com/block/Version-Guard/pkg/telemetry"
	"github.com/block/Version-Guard/pkg/types"
)

// Activity names (stable for replay - following block-am pattern)
const (
	FetchInventoryActivityName = "version-guard.FetchInventory"
	FetchEOLDataActivityName   = "version-guard.FetchEOLData"
	DetectDriftActivityName    = "version-guard.DetectDrift"
	StoreFindingsActivityName  = "version-guard.StoreFindings"
	EmitMetricsActivityName    = "version-guard.EmitMetrics"
)

// Activity input/output types

type FetchInventoryInput struct {
	ScanID       string
	ResourceType types.ResourceType
}

type InventoryResult struct {
	ResourceBatchID string
	Resources       []*types.Resource
}

type FetchEOLInput struct {
	ResourceType    types.ResourceType
	ResourceBatchID string
	Resources       []*types.Resource
}

type EOLResult struct {
	VersionLifecycles map[string]*types.VersionLifecycle // key: "engine:version"
}

//nolint:govet // field alignment sacrificed for logical grouping
type DetectInput struct {
	ResourceBatchID   string
	Resources         []*types.Resource
	VersionLifecycles map[string]*types.VersionLifecycle
}

//nolint:govet // field alignment sacrificed for logical grouping
type DetectResult struct {
	FindingsBatchID string
	FindingsCount   int
	Findings        []*types.Finding
}

type StoreInput struct {
	FindingsBatchID string
	Findings        []*types.Finding
}

//nolint:govet // field alignment sacrificed for logical grouping
type MetricsInput struct {
	FindingsBatchID string
	Findings        []*types.Finding
	ResourceType    types.ResourceType
}

type MetricsResult struct {
	Summary *types.ScanSummary
}

// Activities struct holds dependencies for all activities.
//
// EOLProviders is keyed by the YAML config ID (e.g. "aurora-postgresql",
// "lambda") because each Provider is bound to a single endoflife.date
// product and schema at construction time. FetchEOLData routes to the
// right Provider via the resource type carried on FetchEOLInput, so a
// scan touching N resource types fans out across N Providers — each
// hitting only its own product's cycles.
type Activities struct {
	InventorySources map[types.ResourceType]inventory.InventorySource
	EOLProviders     map[types.ResourceType]eol.Provider
	Policy           policy.VersionPolicy
	Store            store.Store
	resourceCache    sync.Map
}

// NewActivities creates a new Activities instance with dependencies.
// eolProviders MUST contain one entry per resource type the scan will
// touch; FetchEOLData fails with an explicit error if a resource type's
// provider is missing rather than silently producing UNKNOWN findings.
func NewActivities(
	inventorySources map[types.ResourceType]inventory.InventorySource,
	eolProviders map[types.ResourceType]eol.Provider,
	policy policy.VersionPolicy,
	store store.Store,
) *Activities {
	return &Activities{
		InventorySources: inventorySources,
		EOLProviders:     eolProviders,
		Policy:           policy,
		Store:            store,
	}
}

func (a *Activities) loadResources(batchID string, fallback []*types.Resource) ([]*types.Resource, error) {
	if batchID == "" {
		return fallback, nil
	}
	v, ok := a.resourceCache.Load(batchID)
	if !ok {
		return nil, fmt.Errorf("resource batch %q not found", batchID)
	}
	resources, ok := v.([]*types.Resource)
	if !ok {
		return nil, fmt.Errorf("resource batch %q has invalid type", batchID)
	}
	return resources, nil
}

func (a *Activities) loadFindings(batchID string, fallback []*types.Finding) ([]*types.Finding, error) {
	if batchID == "" {
		return fallback, nil
	}
	v, ok := a.resourceCache.Load(batchID)
	if !ok {
		return nil, fmt.Errorf("findings batch %q not found", batchID)
	}
	findings, ok := v.([]*types.Finding)
	if !ok {
		return nil, fmt.Errorf("findings batch %q has invalid type", batchID)
	}
	return findings, nil
}

// FetchInventory fetches all resources of a given type from the inventory source
func (a *Activities) FetchInventory(ctx context.Context, input FetchInventoryInput) (*InventoryResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Fetching inventory", "resourceType", input.ResourceType)

	source, ok := a.InventorySources[input.ResourceType]
	if !ok {
		return nil, fmt.Errorf("no inventory source registered for resource type: %s", input.ResourceType)
	}

	resources, err := source.ListResources(ctx, input.ResourceType)
	if err != nil {
		return nil, err
	}

	logger.Info("Inventory fetched", "count", len(resources))

	if input.ScanID == "" {
		return &InventoryResult{Resources: resources}, nil
	}

	// Cache key must include the resource type — the orchestrator
	// fans out one detection workflow per resource type and they all
	// share the same ScanID. Keying solely on ScanID caused parallel
	// workflows to overwrite each other's resources, so e.g. the
	// memcached detection would read the valkey resources back and
	// the per-type counts ended up scrambled.
	batchID := input.ScanID + ":" + string(input.ResourceType)
	a.resourceCache.Store(batchID, resources)
	return &InventoryResult{ResourceBatchID: batchID}, nil
}

// FetchEOLData fetches EOL lifecycle information for all resource versions.
//
// The provider is selected by input.ResourceType — each Provider is
// bound to a single endoflife.date product, so we MUST use the one
// matching this resource type. A missing entry is a configuration bug
// (the server-side detector loop should have created a provider for
// every configured resource), so we fail loud rather than silently
// returning UNKNOWN findings — that was the symptom of the regression
// fixed in this commit.
func (a *Activities) FetchEOLData(ctx context.Context, input FetchEOLInput) (*EOLResult, error) {
	resources, err := a.loadResources(input.ResourceBatchID, input.Resources)
	if err != nil {
		return nil, err
	}

	logger := activity.GetLogger(ctx)
	logger.Info("Fetching EOL data", "resourceCount", len(resources), "resourceType", input.ResourceType)

	provider, ok := a.EOLProviders[input.ResourceType]
	if !ok {
		return nil, fmt.Errorf("no EOL provider configured for resource type %q", input.ResourceType)
	}

	lifecycles := make(map[string]*types.VersionLifecycle)

	// Fetch lifecycle for each unique engine:version combination
	seen := make(map[string]bool)
	for _, resource := range resources {
		key := resource.Engine + ":" + resource.CurrentVersion
		if seen[key] {
			continue
		}
		seen[key] = true

		lifecycle, err := provider.GetVersionLifecycle(ctx, resource.Engine, resource.CurrentVersion)
		if err != nil {
			logger.Warn("Failed to get lifecycle", "engine", resource.Engine, "version", resource.CurrentVersion, "error", err)
			// Continue with other versions
			continue
		}

		lifecycles[key] = lifecycle
	}

	logger.Info("EOL data fetched", "versionCount", len(lifecycles))

	return &EOLResult{
		VersionLifecycles: lifecycles,
	}, nil
}

// DetectDrift analyzes resources against EOL data and classifies them
func (a *Activities) DetectDrift(ctx context.Context, input DetectInput) (*DetectResult, error) {
	resources, err := a.loadResources(input.ResourceBatchID, input.Resources)
	if err != nil {
		return nil, err
	}

	logger := activity.GetLogger(ctx)
	logger.Info("Detecting version drift", "resourceCount", len(resources))

	var findings []*types.Finding

	for _, resource := range resources {
		key := resource.Engine + ":" + resource.CurrentVersion
		lifecycle, ok := input.VersionLifecycles[key]
		if !ok {
			// No lifecycle data - create unknown finding
			lifecycle = &types.VersionLifecycle{
				Version:     resource.CurrentVersion,
				Engine:      resource.Engine,
				IsSupported: false,
			}
		}

		// Classify using policy
		status := a.Policy.Classify(resource, lifecycle)
		message := a.Policy.GetMessage(resource, lifecycle, status)

		// Create finding. Name, account, and region (when configured) are
		// part of resource.Extra and propagate through verbatim.
		finding := &types.Finding{
			ResourceID:     resource.ID,
			ResourceType:   resource.Type,
			Service:        resource.Service,
			CloudProvider:  resource.CloudProvider,
			CurrentVersion: resource.CurrentVersion,
			Engine:         resource.Engine,
			Status:         status,
			Message:        message,
			EOLDate:        lifecycle.EOLDate,
			Tags:           resource.Tags,
			Extra:          resource.Extra,
		}

		findings = append(findings, finding)
	}

	logger.Info("Drift detection complete", "findingsCount", len(findings))

	if input.ResourceBatchID != "" {
		a.resourceCache.Delete(input.ResourceBatchID)
		findingsBatchID := input.ResourceBatchID + "-findings"
		a.resourceCache.Store(findingsBatchID, findings)
		return &DetectResult{FindingsBatchID: findingsBatchID, FindingsCount: len(findings)}, nil
	}

	return &DetectResult{
		Findings:      findings,
		FindingsCount: len(findings),
	}, nil
}

// StoreFindings persists findings to the store
func (a *Activities) StoreFindings(ctx context.Context, input StoreInput) error {
	findings, err := a.loadFindings(input.FindingsBatchID, input.Findings)
	if err != nil {
		return err
	}

	logger := activity.GetLogger(ctx)
	logger.Info("Storing findings", "count", len(findings))

	if err := a.Store.SaveFindings(ctx, findings); err != nil {
		return err
	}

	logger.Info("Findings stored successfully")
	return nil
}

// EmitMetrics calculates summary statistics and emits metrics
func (a *Activities) EmitMetrics(ctx context.Context, input MetricsInput) (*MetricsResult, error) {
	findings, err := a.loadFindings(input.FindingsBatchID, input.Findings)
	if err != nil {
		return nil, err
	}

	logger := activity.GetLogger(ctx)
	logger.Info("Emitting metrics", "findingsCount", len(findings))

	// Calculate summary
	summary := &types.ScanSummary{
		TotalResources: len(findings),
	}

	for _, f := range findings {
		switch f.Status {
		case types.StatusRed:
			summary.RedCount++
		case types.StatusYellow:
			summary.YellowCount++
		case types.StatusGreen:
			summary.GreenCount++
		case types.StatusUnknown:
			summary.UnknownCount++
		}
	}

	if summary.TotalResources > 0 {
		summary.CompliancePercentage = (float64(summary.GreenCount) / float64(summary.TotalResources)) * 100
	}

	logger.Info("Metrics calculated",
		"total", summary.TotalResources,
		"red", summary.RedCount,
		"yellow", summary.YellowCount,
		"green", summary.GreenCount,
		"compliance", summary.CompliancePercentage)

	telemetry.RecordDetectionSummary(input.ResourceType, summary)

	if input.FindingsBatchID != "" {
		a.resourceCache.Delete(input.FindingsBatchID)
	}

	return &MetricsResult{
		Summary: summary,
	}, nil
}
