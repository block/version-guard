package wiz

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/block/Version-Guard/pkg/registry"
	"github.com/block/Version-Guard/pkg/types"
)

// eksRequiredColumns lists the CSV header names that must be present
// in a Wiz EKS saved report.
var eksRequiredColumns = []string{
	colHeaderExternalID,
	colHeaderName,
	colHeaderNativeType,
	colHeaderAccountID,
	colHeaderVersion,
	colHeaderRegion,
	colHeaderTags,
}

// EKSInventorySource fetches EKS cluster inventory from Wiz saved reports
//
//nolint:govet // field alignment sacrificed for logical grouping
type EKSInventorySource struct {
	client         *Client
	reportID       string
	registryClient registry.Client // Optional: for service attribution when tags are missing
	tagConfig      *TagConfig      // Configurable tag key mappings
}

// NewEKSInventorySource creates a new Wiz-based EKS inventory source with default tag configuration.
// Use WithTagConfig() to customize tag key mappings.
func NewEKSInventorySource(client *Client, reportID string) *EKSInventorySource {
	return &EKSInventorySource{
		client:    client,
		reportID:  reportID,
		tagConfig: DefaultTagConfig(),
	}
}

// WithRegistryClient adds optional registry integration for service attribution.
// When tags are missing, the registry will be queried to map AWS account → service.
func (s *EKSInventorySource) WithRegistryClient(registryClient registry.Client) *EKSInventorySource {
	s.registryClient = registryClient
	return s
}

// WithTagConfig sets custom tag key mappings for extracting metadata.
// If not called, uses DefaultTagConfig() with standard AWS tag conventions.
func (s *EKSInventorySource) WithTagConfig(config *TagConfig) *EKSInventorySource {
	if config != nil {
		s.tagConfig = config
	}
	return s
}

// Name returns the name of this inventory source
func (s *EKSInventorySource) Name() string {
	return "wiz-eks"
}

// CloudProvider returns the cloud provider this source supports
func (s *EKSInventorySource) CloudProvider() types.CloudProvider {
	return types.CloudProviderAWS
}

// ListResources fetches all EKS clusters from Wiz
func (s *EKSInventorySource) ListResources(ctx context.Context, resourceType types.ResourceType) ([]*types.Resource, error) {
	if resourceType != types.ResourceTypeEKS {
		return nil, fmt.Errorf("unsupported resource type: %s (only EKS supported)", resourceType)
	}

	// Use shared helper to parse Wiz report
	return parseWizReport(
		ctx,
		s.client,
		s.reportID,
		eksRequiredColumns,
		func(cols columnIndex, row []string) bool {
			// Filter for EKS clusters - nativeType should be "cluster"
			return isEKSResource(cols.col(row, colHeaderNativeType))
		},
		s.parseEKSRow,
	)
}

// GetResource fetches a specific EKS cluster by ARN
func (s *EKSInventorySource) GetResource(ctx context.Context, resourceType types.ResourceType, id string) (*types.Resource, error) {
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

// parseEKSRow parses a single CSV row into a Resource
func (s *EKSInventorySource) parseEKSRow(ctx context.Context, cols columnIndex, row []string) (*types.Resource, error) {
	// Extract basic fields
	externalID, err := cols.require(row, colHeaderExternalID)
	if err != nil {
		return nil, fmt.Errorf("missing external ID")
	}

	accountID, err := cols.require(row, colHeaderAccountID)
	if err != nil {
		return nil, fmt.Errorf("missing AWS account ID")
	}

	region, err := cols.require(row, colHeaderRegion)
	if err != nil {
		return nil, fmt.Errorf("missing region")
	}

	version := cols.col(row, colHeaderVersion)
	// Normalize version to include "k8s-" prefix for consistency
	if !strings.HasPrefix(version, "k8s-") && version != "" {
		version = "k8s-" + version
	}

	// Parse tags
	tagsJSON := cols.col(row, colHeaderTags)
	tags, err := ParseTags(tagsJSON)
	if err != nil {
		// Non-fatal, just use empty tags
		tags = make(map[string]string)
	}

	// Get cluster name from CSV
	clusterName := cols.col(row, colHeaderName)
	if clusterName == "" {
		// Fallback: construct a synthetic name from external ID
		clusterName = fmt.Sprintf("eks-cluster-%s", externalID[:12])
	}

	// Construct a pseudo-ARN since we don't have the real ARN
	// This helps maintain consistency with other AWS resources
	pseudoARN := fmt.Sprintf("arn:aws:eks:%s:%s:cluster/%s", region, accountID, clusterName)

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

		// Final fallback: extract from cluster name
		if service == "" {
			service = extractServiceFromName(clusterName)
		}
	}

	// Extract brand (using configurable tag keys)
	brand := s.tagConfig.GetBrandTag(tags)
	// Note: This report format doesn't have a products column for brand inference

	resource := &types.Resource{
		ID:             pseudoARN,
		Name:           clusterName,
		Type:           types.ResourceTypeEKS,
		CloudProvider:  types.CloudProviderAWS,
		Service:        service,
		CloudAccountID: accountID,
		CloudRegion:    region,
		Brand:          brand,
		CurrentVersion: version,
		Engine:         "kubernetes", // EKS uses kubernetes engine
		Tags:           tags,
		DiscoveredAt:   time.Now(),
	}

	return resource, nil
}

// isEKSResource checks if a Wiz native type represents an EKS cluster
func isEKSResource(nativeType string) bool {
	nativeType = strings.ToLower(nativeType)
	// In the EKS versions report, nativeType is simply "cluster"
	// This is EKS-specific, so we accept it
	return nativeType == "cluster" ||
		strings.Contains(nativeType, "eks") ||
		strings.Contains(nativeType, "eks-cluster") ||
		strings.Contains(nativeType, "eks_cluster")
}
