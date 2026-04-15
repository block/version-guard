package wiz

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/block/Version-Guard/pkg/registry"
	"github.com/block/Version-Guard/pkg/types"
)

// ElastiCacheInventorySource fetches ElastiCache cluster inventory from Wiz saved reports
type ElastiCacheInventorySource struct {
	client         *Client
	reportID       string
	registryClient registry.Client // Optional: for service attribution when tags are missing
	tagConfig      *TagConfig      // Configurable tag key mappings
	logger         *slog.Logger
}

// NewElastiCacheInventorySource creates a new Wiz-based ElastiCache inventory source with default tag configuration.
// Use WithTagConfig() to customize tag key mappings.
func NewElastiCacheInventorySource(client *Client, reportID string, logger *slog.Logger) *ElastiCacheInventorySource {
	if logger == nil {
		logger = slog.Default()
	}
	return &ElastiCacheInventorySource{
		client:    client,
		reportID:  reportID,
		tagConfig: DefaultTagConfig(),
		logger:    logger,
	}
}

// WithRegistryClient adds optional registry integration for service attribution.
// When tags are missing, the registry will be queried to map AWS account → service.
func (s *ElastiCacheInventorySource) WithRegistryClient(registryClient registry.Client) *ElastiCacheInventorySource {
	s.registryClient = registryClient
	return s
}

// WithTagConfig sets custom tag key mappings for extracting metadata.
// If not called, uses DefaultTagConfig() with standard AWS tag conventions.
func (s *ElastiCacheInventorySource) WithTagConfig(config *TagConfig) *ElastiCacheInventorySource {
	if config != nil {
		s.tagConfig = config
	}
	return s
}

// Name returns the name of this inventory source
func (s *ElastiCacheInventorySource) Name() string {
	return "wiz-elasticache"
}

// CloudProvider returns the cloud provider this source supports
func (s *ElastiCacheInventorySource) CloudProvider() types.CloudProvider {
	return types.CloudProviderAWS
}

// ListResources fetches all ElastiCache resources from Wiz
func (s *ElastiCacheInventorySource) ListResources(ctx context.Context, resourceType types.ResourceType) ([]*types.Resource, error) {
	if resourceType != types.ResourceTypeElastiCache {
		return nil, fmt.Errorf("unsupported resource type: %s (only ELASTICACHE supported)", resourceType)
	}

	// Use shared helper to parse Wiz report
	return parseWizReport(
		ctx,
		s.client,
		s.reportID,
		auroraRequiredColumns, // Same columns as Aurora
		func(cols columnIndex, row []string) bool {
			// Filter for ElastiCache resources only
			return isElastiCacheResource(cols.col(row, colHeaderNativeType))
		},
		s.parseElastiCacheRow,
		s.logger,
	)
}

// GetResource fetches a specific ElastiCache resource by ARN
func (s *ElastiCacheInventorySource) GetResource(ctx context.Context, resourceType types.ResourceType, id string) (*types.Resource, error) {
	// For Wiz source, we fetch all and filter
	resources, err := s.ListResources(ctx, resourceType)
	if err != nil {
		return nil, err
	}

	for _, resource := range resources {
		if resource.ID == id {
			return resource, nil
		}
	}

	return nil, fmt.Errorf("resource not found: %s", id)
}

// parseElastiCacheRow parses a single CSV row into a Resource
func (s *ElastiCacheInventorySource) parseElastiCacheRow(ctx context.Context, cols columnIndex, row []string) (*types.Resource, error) {
	return parseAWSResourceRow(ctx, cols, row, types.ResourceTypeElastiCache, normalizeElastiCacheKind, s.tagConfig, s.registryClient)
}

// normalizeElastiCacheKind converts Wiz typeFields.kind values to standard engine names
// Wiz uses simple kind values: "Redis", "Memcached", "Valkey"
func normalizeElastiCacheKind(kind string) string {
	return strings.ToLower(kind)
}

// isElastiCacheResource checks if a Wiz native type represents an ElastiCache cluster or instance.
// Wiz nativeType examples:
//   - "elastiCache/Redis/cluster", "elastiCache/Redis/instance"
//   - "elastiCache/Memcached/cluster", "elastiCache/Valkey/instance"
//
// We exclude non-versioned types like "elasticache#snapshot", "elasticache#user", "elasticache#usergroup".
func isElastiCacheResource(nativeType string) bool {
	nativeType = strings.ToLower(nativeType)
	return strings.HasPrefix(nativeType, "elasticache/") &&
		(strings.HasSuffix(nativeType, "/cluster") || strings.HasSuffix(nativeType, "/instance"))
}
