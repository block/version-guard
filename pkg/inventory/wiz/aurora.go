package wiz

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/pkg/errors"

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
	resourceARN, err := cols.require(row, colHeaderExternalID)
	if err != nil {
		return nil, fmt.Errorf("missing ARN")
	}

	// Parse ARN
	parsedARN, err := arn.Parse(resourceARN)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid ARN: %s", resourceARN)
	}

	// Extract metadata
	resourceName := cols.col(row, colHeaderName)
	accountID := cols.col(row, colHeaderAccountID)
	if accountID == "" {
		accountID = parsedARN.AccountID
	}

	engine := normalizeEngineKind(cols.col(row, colHeaderEngineKind))
	version := cols.col(row, colHeaderVersion)
	region := cols.col(row, colHeaderRegion)

	// Parse tags
	tagsJSON := cols.col(row, colHeaderTags)
	tags, err := ParseTags(tagsJSON)
	if err != nil {
		// Non-fatal, just use empty tags
		tags = make(map[string]string)
	}

	// Extract service name from tags (using configurable tag keys)
	service := s.tagConfig.GetAppTag(tags)
	if service == "" {
		// Try registry lookup by AWS account (if registry is configured)
		if s.registryClient != nil {
			if serviceInfo, err := s.registryClient.GetServiceByAWSAccount(ctx, accountID, region); err == nil {
				service = serviceInfo.ServiceName
			}
			// Ignore registry errors - fall through to name parsing
		}

		// Final fallback: extract from resource name or ARN
		if service == "" {
			service = extractServiceFromName(resourceName)
		}
	}

	// Extract brand (using configurable tag keys)
	brand := s.tagConfig.GetBrandTag(tags)

	resource := &types.Resource{
		ID:             resourceARN,
		Name:           resourceName,
		Type:           types.ResourceTypeAurora,
		CloudProvider:  types.CloudProviderAWS,
		Service:        service,
		CloudAccountID: accountID,
		CloudRegion:    region,
		Brand:          brand,
		CurrentVersion: version,
		Engine:         engine,
		Tags:           tags,
		DiscoveredAt:   time.Now(),
	}

	return resource, nil
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

// extractServiceFromName attempts to extract service name from resource name
// Example: "my-service-prod-cluster" -> "my-service"
func extractServiceFromName(name string) string {
	// Simple heuristic: take everything before first hyphen + environment suffix
	parts := strings.Split(name, "-")
	if len(parts) > 0 {
		// Remove common suffixes
		for i, part := range parts {
			if isEnvironmentSuffix(part) || isCommonSuffix(part) {
				if i > 0 {
					return strings.Join(parts[:i], "-")
				}
				break
			}
		}
	}
	return name
}

// isEnvironmentSuffix checks if a string is a common environment suffix
func isEnvironmentSuffix(s string) bool {
	s = strings.ToLower(s)
	envs := []string{"dev", "development", "staging", "stage", "prod", "production", "test", "qa"}
	for _, env := range envs {
		if s == env {
			return true
		}
	}
	return false
}

// isCommonSuffix checks if a string is a common resource suffix
func isCommonSuffix(s string) bool {
	s = strings.ToLower(s)
	suffixes := []string{"cluster", "db", "database", "rds", "aurora"}
	for _, suffix := range suffixes {
		if s == suffix {
			return true
		}
	}
	return false
}
