package generic

import (
	"context"
	"log/slog"
	"time"

	"github.com/pkg/errors"

	"github.com/block/Version-Guard/pkg/config"
	"github.com/block/Version-Guard/pkg/eol"
	"github.com/block/Version-Guard/pkg/inventory"
	"github.com/block/Version-Guard/pkg/policy"
	"github.com/block/Version-Guard/pkg/types"
)

// Detector is a config-driven generic detector that works for any resource type
type Detector struct {
	config    *config.ResourceConfig
	inventory inventory.InventorySource
	eol       eol.Provider
	policy    policy.VersionPolicy
	logger    *slog.Logger
}

// NewDetector creates a new generic detector from configuration
func NewDetector(
	cfg *config.ResourceConfig,
	inventory inventory.InventorySource,
	eol eol.Provider,
	policy policy.VersionPolicy,
	logger *slog.Logger,
) *Detector {
	if logger == nil {
		logger = slog.Default()
	}
	return &Detector{
		config:    cfg,
		inventory: inventory,
		eol:       eol,
		policy:    policy,
		logger:    logger,
	}
}

// Name returns the name of this detector
func (d *Detector) Name() string {
	return d.config.ID + "-detector"
}

// ResourceType returns the resource type this detector handles
func (d *Detector) ResourceType() types.ResourceType {
	return types.ResourceType(d.config.Type)
}

// Detect scans resources and detects version drift
func (d *Detector) Detect(ctx context.Context) ([]*types.Finding, error) {
	// Step 1: Fetch inventory
	resources, err := d.inventory.ListResources(ctx, d.ResourceType())
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch inventory")
	}

	if len(resources) == 0 {
		// No resources to scan
		return []*types.Finding{}, nil
	}

	// Step 2: For each resource, fetch EOL data and classify
	var findings []*types.Finding
	for _, resource := range resources {
		finding, err := d.detectResource(ctx, resource)
		if err != nil {
			// Log error but continue with other resources
			d.logger.WarnContext(ctx, "failed to detect drift for resource",
				"resource_id", resource.ID,
				"error", err)
			continue
		}

		if finding != nil {
			findings = append(findings, finding)
		}
	}

	return findings, nil
}

// detectResource detects drift for a single resource
func (d *Detector) detectResource(ctx context.Context, resource *types.Resource) (*types.Finding, error) {
	// Fetch EOL data for this version
	lifecycle, err := d.eol.GetVersionLifecycle(ctx, resource.Engine, resource.CurrentVersion)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch EOL data for %s %s",
			resource.Engine, resource.CurrentVersion)
	}

	// Classify using policy
	status := d.policy.Classify(resource, lifecycle)

	// Generate message and recommendation
	message := d.policy.GetMessage(resource, lifecycle, status)
	recommendation := d.policy.GetRecommendation(resource, lifecycle, status)

	// Create finding
	finding := &types.Finding{
		ResourceID:     resource.ID,
		ResourceName:   resource.Name,
		ResourceType:   resource.Type,
		Service:        resource.Service,
		CloudAccountID: resource.CloudAccountID,
		CloudRegion:    resource.CloudRegion,
		CloudProvider:  resource.CloudProvider,
		Brand:          resource.Brand,
		CurrentVersion: resource.CurrentVersion,
		Engine:         resource.Engine,
		Status:         status,
		Message:        message,
		Recommendation: recommendation,
		EOLDate:        lifecycle.EOLDate,
		DetectedAt:     time.Now(),
		UpdatedAt:      time.Now(),
	}

	return finding, nil
}
