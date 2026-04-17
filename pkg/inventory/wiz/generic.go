package wiz

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/block/Version-Guard/pkg/config"
	"github.com/block/Version-Guard/pkg/registry"
	"github.com/block/Version-Guard/pkg/types"
)

const (
	resourceTypeEKS        = "eks"
	resourceTypeOpenSearch = "opensearch"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const discoveredAtKey contextKey = "discovered_at"

// GenericInventorySource is a config-driven Wiz inventory source
// that can handle any resource type based on YAML configuration
type GenericInventorySource struct {
	client         *Client
	config         *config.ResourceConfig
	registryClient registry.Client
	logger         *slog.Logger
}

// NewGenericInventorySource creates a new generic inventory source from config
func NewGenericInventorySource(
	client *Client,
	cfg *config.ResourceConfig,
	registryClient registry.Client,
	logger *slog.Logger,
) *GenericInventorySource {
	if logger == nil {
		logger = slog.Default()
	}
	return &GenericInventorySource{
		client:         client,
		config:         cfg,
		registryClient: registryClient,
		logger:         logger,
	}
}

// Name returns the name of this inventory source
func (s *GenericInventorySource) Name() string {
	return "wiz-" + s.config.ID
}

// CloudProvider returns the cloud provider for this source
func (s *GenericInventorySource) CloudProvider() types.CloudProvider {
	switch s.config.CloudProvider {
	case "aws":
		return types.CloudProviderAWS
	case "gcp":
		return types.CloudProviderGCP
	case "azure":
		return types.CloudProviderAzure
	default:
		return types.CloudProviderAWS // Default to AWS
	}
}

// ListResources fetches resources from Wiz using the configured report
func (s *GenericInventorySource) ListResources(ctx context.Context, resourceType types.ResourceType) ([]*types.Resource, error) {
	// Verify resource type matches config
	if string(resourceType) != s.config.Type {
		return nil, errors.Errorf("unsupported resource type: %s (expected: %s)", resourceType, s.config.Type)
	}

	// Get report ID from WIZ_REPORT_IDS map
	reportID, err := getReportIDFromMap(s.config.ID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get report ID for resource %s", s.config.ID)
	}
	if reportID == "" {
		return nil, errors.Errorf("no report ID configured for resource %s in WIZ_REPORT_IDS map", s.config.ID)
	}

	// Determine required columns from field mappings
	requiredColumns := s.getRequiredColumns()

	// Filter function: check nativeType pattern
	filterRow := func(cols columnIndex, row []string) bool {
		nativeType := cols.col(row, colHeaderNativeType)
		return s.matchesNativeTypePattern(nativeType)
	}

	// Parser function: parse row into Resource
	parseRow := func(ctx context.Context, cols columnIndex, row []string) (*types.Resource, error) {
		return s.parseResourceRow(ctx, cols, row)
	}

	// Use shared helper to parse Wiz report
	return parseWizReport(
		ctx,
		s.client,
		reportID,
		requiredColumns,
		filterRow,
		parseRow,
		s.logger,
	)
}

// GetResource fetches a single resource by ID
func (s *GenericInventorySource) GetResource(ctx context.Context, resourceType types.ResourceType, id string) (*types.Resource, error) {
	resources, err := s.ListResources(ctx, resourceType)
	if err != nil {
		return nil, err
	}

	for _, r := range resources {
		if r.ID == id {
			return r, nil
		}
	}

	return nil, errors.Errorf("resource not found: %s", id)
}

// getRequiredColumns builds list of required CSV columns from field mappings
func (s *GenericInventorySource) getRequiredColumns() []string {
	columns := []string{
		colHeaderExternalID,
		colHeaderName,
		colHeaderNativeType,
		colHeaderAccountID,
		colHeaderRegion,
		colHeaderTags,
	}

	// Add version if mapped
	if versionField, ok := s.config.Inventory.FieldMappings["version"]; ok && versionField != "" {
		columns = append(columns, versionField)
	}

	// Add engine if mapped
	if engineField, ok := s.config.Inventory.FieldMappings["engine"]; ok && engineField != "" {
		columns = append(columns, engineField)
	}

	return columns
}

// matchesNativeTypePattern checks if nativeType matches the configured pattern.
// Supports exact match, wildcard patterns (e.g., "elastiCache/*/cluster"),
// and pipe-delimited alternatives (e.g., "elasticSearchService|OpenSearch Domain").
func (s *GenericInventorySource) matchesNativeTypePattern(nativeType string) bool {
	pattern := s.config.Inventory.NativeTypePattern

	// Handle pipe-delimited alternatives (e.g., "typeA|typeB")
	if strings.Contains(pattern, "|") {
		for _, alt := range strings.Split(pattern, "|") {
			if nativeType == alt {
				return true
			}
		}
		return false
	}

	// Handle wildcard patterns (e.g., "elastiCache/*/cluster")
	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "/")
		typeParts := strings.Split(nativeType, "/")

		if len(parts) != len(typeParts) {
			return false
		}

		for i, part := range parts {
			if part != "*" && part != typeParts[i] {
				return false
			}
		}
		return true
	}

	// Exact match
	return nativeType == pattern
}

// parseResourceRow parses a CSV row into a Resource using field mappings
func (s *GenericInventorySource) parseResourceRow(
	ctx context.Context,
	cols columnIndex,
	row []string,
) (*types.Resource, error) {
	// Extract required fields
	externalID, err := cols.require(row, colHeaderExternalID)
	if err != nil {
		return nil, err
	}

	name := cols.col(row, colHeaderName)
	if name == "" {
		name = externalID // Fallback to ID if name is empty
	}

	accountID := cols.col(row, colHeaderAccountID)

	region := cols.col(row, colHeaderRegion)

	// Extract version from mapped field
	version := ""
	if versionField, ok := s.config.Inventory.FieldMappings["version"]; ok {
		version = cols.col(row, versionField)
	}

	// Extract engine from mapped field
	engine := ""
	if engineField, ok := s.config.Inventory.FieldMappings["engine"]; ok {
		engine = cols.col(row, engineField)
	}

	// For EKS, default to "eks" if no engine field is mapped
	if s.config.Type == resourceTypeEKS && engine == "" {
		engine = resourceTypeEKS
	}

	// Normalize engine
	engine = normalizeEngine(engine, s.config.Type)

	// OpenSearch-specific: normalize version and detect legacy Elasticsearch
	if s.config.Type == resourceTypeOpenSearch {
		version = normalizeOpenSearchVersion(version)
		engine = detectOpenSearchEngine(version)
	}

	// Parse tags to extract service, brand
	tagsJSON := cols.col(row, colHeaderTags)
	tags, err := ParseTags(tagsJSON)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to parse tags",
			"resource_id", externalID,
			"error", err)
		tags = nil
	}

	// Extract service and brand from tags using TagConfig
	tagConfig := DefaultTagConfig()
	service := tagConfig.GetAppTag(tags)
	brand := tagConfig.GetBrandTag(tags)

	// If no service in tags, try to extract from resource name
	if service == "" {
		service = extractServiceFromName(name)
	}

	// Build resource
	discoveredAt := time.Now()
	if ctxTime, ok := ctx.Value(discoveredAtKey).(time.Time); ok {
		discoveredAt = ctxTime
	}

	resource := &types.Resource{
		ID:             externalID,
		Name:           name,
		Type:           types.ResourceType(s.config.Type),
		CloudProvider:  s.CloudProvider(),
		CloudAccountID: accountID,
		CloudRegion:    region,
		CurrentVersion: version,
		Engine:         engine,
		Service:        service,
		Brand:          brand,
		Tags:           tags,
		DiscoveredAt:   discoveredAt,
	}

	return resource, nil
}

// getReportIDFromMap reads the WIZ_REPORT_IDS JSON map and returns the report ID for the given resource
func getReportIDFromMap(resourceID string) (string, error) {
	// Read WIZ_REPORT_IDS environment variable
	reportIDsJSON := os.Getenv("WIZ_REPORT_IDS")
	if reportIDsJSON == "" {
		return "", errors.New("WIZ_REPORT_IDS environment variable not set")
	}

	// Parse JSON map
	var reportIDs map[string]string
	if err := json.Unmarshal([]byte(reportIDsJSON), &reportIDs); err != nil {
		return "", errors.Wrap(err, "failed to parse WIZ_REPORT_IDS JSON")
	}

	// Get report ID for this resource
	reportID, ok := reportIDs[resourceID]
	if !ok {
		return "", nil // Not found in map, but not an error - let caller decide
	}

	return reportID, nil
}

// normalizeOpenSearchVersion strips engine prefixes from OpenSearch/Elasticsearch
// version strings (e.g., "OpenSearch_2.13" → "2.13", "Elasticsearch_7.10" → "7.10").
func normalizeOpenSearchVersion(version string) string {
	version = strings.TrimPrefix(version, "OpenSearch_")
	version = strings.TrimPrefix(version, "Elasticsearch_")
	return version
}

// detectOpenSearchEngine returns "elasticsearch" for legacy Elasticsearch versions
// (5.x, 6.x, 7.x) and "opensearch" for OpenSearch versions (1.x, 2.x, 3.x+).
// OpenSearch forked from Elasticsearch 7.10, so versions ≤7.x are Elasticsearch.
func detectOpenSearchEngine(version string) string {
	if version == "" {
		return resourceTypeOpenSearch
	}
	major := strings.SplitN(version, ".", 2)[0]
	switch major {
	case "5", "6", "7":
		return "elasticsearch"
	default:
		return resourceTypeOpenSearch
	}
}

// normalizeEngine normalizes engine names based on resource type
func normalizeEngine(engine, resourceType string) string {
	engine = strings.ToLower(strings.TrimSpace(engine))

	// Handle type-specific normalization
	switch resourceType {
	case "aurora":
		// AuroraMySQL → aurora-mysql
		// AuroraPostgreSQL → aurora-postgresql
		if strings.Contains(engine, "aurora") {
			if strings.Contains(engine, "mysql") {
				return "aurora-mysql"
			}
			if strings.Contains(engine, "postgres") {
				return "aurora-postgresql"
			}
		}
	case "elasticache":
		// Redis → redis, Valkey → valkey, Memcached → memcached
		return engine
	case resourceTypeEKS:
		// Kubernetes → eks
		if strings.Contains(engine, "k8s") || strings.Contains(engine, "kubernetes") {
			return resourceTypeEKS
		}
	}

	return engine
}
