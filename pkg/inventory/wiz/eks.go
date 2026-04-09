package wiz

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/block/Version-Guard/pkg/registry"
	"github.com/block/Version-Guard/pkg/types"
)

// Wiz CSV column indices for EKS clusters
// This report uses a different format than the Aurora report
const (
	colEKSExternalID     = 0 // External ID (hash of cluster ID)
	colEKSName           = 1 // Cluster name
	colEKSNativeType     = 2 // Native type (should be "cluster")
	colEKSAccountID      = 3 // cloudAccount.externalId (AWS Account ID)
	colEKSVersion        = 4 // versionDetails.version (Kubernetes version)
	colEKSRegion         = 5 // region
	colEKSTags           = 6 // tags (JSON)
	colEKSTypeFieldsKind = 7 // typeFields.kind
)

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
//
//nolint:dupl // acceptable duplication with Aurora inventory source
func (s *EKSInventorySource) ListResources(ctx context.Context, resourceType types.ResourceType) ([]*types.Resource, error) {
	if resourceType != types.ResourceTypeEKS {
		return nil, fmt.Errorf("unsupported resource type: %s (only EKS supported)", resourceType)
	}

	// Fetch report data
	rows, err := s.client.GetReportData(ctx, s.reportID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch Wiz report data")
	}

	if len(rows) < 2 {
		// Empty report (only header row)
		return []*types.Resource{}, nil
	}

	// Skip header row, parse data rows
	var resources []*types.Resource
	for i, row := range rows[1:] {
		// Ensure row has minimum required columns
		if len(row) < colEKSTypeFieldsKind+1 {
			// Skip malformed rows
			continue
		}

		// Filter for EKS clusters - nativeType should be "cluster"
		nativeType := row[colEKSNativeType]
		if !isEKSResource(nativeType) {
			continue
		}

		resource, err := s.parseEKSRow(ctx, row)
		if err != nil {
			// Log error but continue processing other rows
			// TODO: wire through proper structured logger (e.g., *slog.Logger)
			log.Printf("WARN: row %d: failed to parse EKS resource: %v", i+1, err)
			continue
		}

		if resource != nil {
			resources = append(resources, resource)
		}
	}

	return resources, nil
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
func (s *EKSInventorySource) parseEKSRow(ctx context.Context, row []string) (*types.Resource, error) {
	// Extract basic fields
	externalID := row[colEKSExternalID]
	if externalID == "" {
		return nil, fmt.Errorf("missing external ID")
	}

	accountID := row[colEKSAccountID]
	if accountID == "" {
		return nil, fmt.Errorf("missing AWS account ID")
	}

	region := row[colEKSRegion]
	if region == "" {
		return nil, fmt.Errorf("missing region")
	}

	version := row[colEKSVersion]
	// Normalize version to include "k8s-" prefix for consistency
	if !strings.HasPrefix(version, "k8s-") && version != "" {
		version = "k8s-" + version
	}

	// Parse tags
	tagsJSON := row[colEKSTags]
	tags, err := ParseTags(tagsJSON)
	if err != nil {
		// Non-fatal, just use empty tags
		tags = make(map[string]string)
	}

	// Get cluster name from CSV
	clusterName := row[colEKSName]
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
