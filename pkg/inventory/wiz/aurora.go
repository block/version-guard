package wiz

import (
	"context"
	"fmt"
	"strings"

	"github.com/block/Version-Guard/pkg/registry"
	"github.com/block/Version-Guard/pkg/types"
)

// auroraRequiredColumns lists the CSV header names that must be present
// in a Wiz Aurora/RDS saved report.
var auroraRequiredColumns = []string{
	colHeaderExternalID,
	colHeaderName,
	colHeaderNativeType,
	colHeaderAccountID,
	colHeaderVersion,
	colHeaderRegion,
	colHeaderTags,
	colHeaderEngineKind,
}

// AuroraInventorySource fetches Aurora/RDS cluster inventory from Wiz saved reports
//
//nolint:govet // field alignment sacrificed for logical grouping
type AuroraInventorySource struct {
	client         *Client
	reportID       string
	registryClient registry.Client // Optional: for service attribution when tags are missing
	tagConfig      *TagConfig      // Configurable tag key mappings
}

// NewAuroraInventorySource creates a new Wiz-based Aurora inventory source with default tag configuration.
// Use WithTagConfig() to customize tag key mappings.
func NewAuroraInventorySource(client *Client, reportID string) *AuroraInventorySource {
	return &AuroraInventorySource{
		client:    client,
		reportID:  reportID,
		tagConfig: DefaultTagConfig(),
	}
}

// WithRegistryClient adds optional registry integration for service attribution.
// When tags are missing, the registry will be queried to map AWS account → service.
func (s *AuroraInventorySource) WithRegistryClient(registryClient registry.Client) *AuroraInventorySource {
	s.registryClient = registryClient
	return s
}

// WithTagConfig sets custom tag key mappings for extracting metadata.
// If not called, uses DefaultTagConfig() with standard AWS tag conventions.
func (s *AuroraInventorySource) WithTagConfig(config *TagConfig) *AuroraInventorySource {
	if config != nil {
		s.tagConfig = config
	}
	return s
}

// Name returns the name of this inventory source
func (s *AuroraInventorySource) Name() string {
	return "wiz-aurora"
}

// CloudProvider returns the cloud provider this source supports
func (s *AuroraInventorySource) CloudProvider() types.CloudProvider {
	return types.CloudProviderAWS
}

// ListResources fetches all Aurora resources from Wiz
func (s *AuroraInventorySource) ListResources(ctx context.Context, resourceType types.ResourceType) ([]*types.Resource, error) {
	if resourceType != types.ResourceTypeAurora {
		return nil, fmt.Errorf("unsupported resource type: %s (only AURORA supported)", resourceType)
	}

	// Use shared helper to parse Wiz report
	return parseWizReport(
		ctx,
		s.client,
		s.reportID,
		auroraRequiredColumns,
		func(cols columnIndex, row []string) bool {
			// Filter for Aurora clusters only
			return isAuroraResource(cols.col(row, colHeaderNativeType))
		},
		s.parseAuroraRow,
	)
}

// GetResource fetches a specific Aurora resource by ARN
func (s *AuroraInventorySource) GetResource(ctx context.Context, resourceType types.ResourceType, id string) (*types.Resource, error) {
	// For Wiz source, we fetch all and filter
	// In production, you might want to optimize this with GraphQL API
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

// parseAuroraRow parses a single CSV row into a Resource
func (s *AuroraInventorySource) parseAuroraRow(ctx context.Context, cols columnIndex, row []string) (*types.Resource, error) {
	return parseAWSResourceRow(ctx, cols, row, types.ResourceTypeAurora, normalizeEngineKind, s.tagConfig, s.registryClient)
}

// normalizeEngineKind converts Wiz typeFields.kind values to standard engine names
// e.g., "AmazonAuroraMySQL" -> "aurora-mysql", "AmazonAuroraPostgreSQL" -> "aurora-postgresql"
func normalizeEngineKind(kind string) string {
	switch strings.ToLower(kind) {
	case "amazonauroramysql":
		return "aurora-mysql"
	case "amazonaurorapostgresql":
		return "aurora-postgresql"
	default:
		return strings.ToLower(kind)
	}
}

// isAuroraResource checks if a Wiz native type represents an Aurora cluster
func isAuroraResource(nativeType string) bool {
	nativeType = strings.ToLower(nativeType)
	return strings.Contains(nativeType, "aurora") ||
		strings.Contains(nativeType, "rds-cluster") ||
		strings.Contains(nativeType, "rds_cluster")
}
