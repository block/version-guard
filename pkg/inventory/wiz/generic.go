package wiz

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/pkg/errors"

	"github.com/block/Version-Guard/pkg/config"
	"github.com/block/Version-Guard/pkg/registry"
	"github.com/block/Version-Guard/pkg/types"
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

// ListResources fetches resources from Wiz using the configured report.
// The resourceType parameter is accepted for interface compatibility but
// the source uses its own config type internally.
func (s *GenericInventorySource) ListResources(ctx context.Context, resourceType types.ResourceType) ([]*types.Resource, error) {
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

// wellKnownFieldMappingKeys are the YAML field_mappings keys whose
// values flow into typed fields on Resource. The set is intentionally
// minimal: only the values the system itself depends on (identity,
// version-lookup keys, and tags for service derivation) are typed.
// Everything else — name, account_id, region, owner, cost_center, ... —
// is routed verbatim into Resource.Extra under its YAML logical name,
// so adding new per-resource attributes is a YAML-only change.
var wellKnownFieldMappingKeys = map[string]struct{}{
	"resource_id": {},
	"version":     {},
	"engine":      {},
	"tags":        {},
}

var awsRegionPattern = regexp.MustCompile(`^[a-z]{2}(?:-[a-z0-9]+)+-\d+$`)

// column returns the CSV column declared in the YAML for the given
// mapping key, or "" when the key is not declared. Required and
// optional mappings are checked together; a mapping is valid wherever
// it lives in the YAML. The loader has already rejected duplicates,
// so at most one of the two maps will hit.
//
// There is no Go-side default — every Wiz column name flows from the
// YAML so that adding a resource (or routing one through a different
// Wiz column, e.g. EKS using "providerUniqueId" instead of
// "externalId") is a YAML-only change. resource_id is guaranteed
// present by the loader (required_mappings.resource_id is mandatory),
// so callers can pass the result straight to cols.require without a
// nil check.
func (s *GenericInventorySource) column(key string) string {
	if mapped, ok := s.config.Inventory.RequiredMappings[key]; ok && mapped != "" {
		return mapped
	}
	if mapped, ok := s.config.Inventory.FieldMappings[key]; ok && mapped != "" {
		return mapped
	}
	return ""
}

// allMappings returns the union of required_mappings and field_mappings.
// Used by code paths that need to iterate every declared mapping
// regardless of required/optional bucket (column-validator setup, Extra
// population). Duplicate keys are impossible — the loader rejects them.
func (s *GenericInventorySource) allMappings() map[string]string {
	required := s.config.Inventory.RequiredMappings
	optional := s.config.Inventory.FieldMappings
	merged := make(map[string]string, len(required)+len(optional))
	for k, v := range required {
		merged[k] = v
	}
	for k, v := range optional {
		merged[k] = v
	}
	return merged
}

// getRequiredColumns builds the list of CSV columns the parser will read,
// derived entirely from the YAML mappings.
//
// The "nativeType" column is always required because it is used to filter
// rows down to the resource type before any field extraction happens —
// it is a Wiz CSV format invariant, not a user-mapped field.
//
// resource_id is always present (the loader rejects configs without it
// in required_mappings); tags is included only if the YAML declares it.
func (s *GenericInventorySource) getRequiredColumns() []string {
	columns := []string{
		s.column("resource_id"),
		colHeaderNativeType,
	}
	if tagsCol := s.column("tags"); tagsCol != "" {
		columns = append(columns, tagsCol)
	}

	// Add version if mapped
	if v := s.column("version"); v != "" {
		columns = append(columns, v)
	}

	// Add engine if mapped
	if e := s.column("engine"); e != "" {
		columns = append(columns, e)
	}

	// Every non-typed YAML mapping (name, account_id, region, owner,
	// cost_center, ...) needs its CSV column in the required set so
	// the Wiz header validator catches typos at parse start instead
	// of silently producing empty Extra values. The required vs
	// field bucket is irrelevant here — we just want every declared
	// CSV column on the validator's check list.
	for key, col := range s.allMappings() {
		if _, isWellKnown := wellKnownFieldMappingKeys[key]; isWellKnown {
			continue
		}
		if col != "" {
			columns = append(columns, col)
		}
	}

	// If a version transform extracts a JSON field from a column,
	// that column must be in the required set so the header
	// validator catches typos at parse start. Derived from YAML
	// instead of hardcoded for Lambda.
	if vt := s.config.Transforms.Version; vt != nil && vt.ExtractJSONField != nil && vt.ExtractJSONField.FromColumn != "" {
		columns = append(columns, vt.ExtractJSONField.FromColumn)
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

// parseResourceRow parses a CSV row into a Resource using field mappings.
//
// Only typed keys (resource_id, version, engine, tags) are read directly
// onto the Resource. Every other field_mappings entry — including name,
// account_id, region — is routed verbatim into Resource.Extra under its
// YAML logical name. resource_id is the only mapping we strictly require
// to be present in the row; missing values for everything else just
// produce empty strings.
func (s *GenericInventorySource) parseResourceRow(
	ctx context.Context,
	cols columnIndex,
	row []string,
) (*types.Resource, error) {
	resourceID, err := cols.require(row, s.column("resource_id"))
	if err != nil {
		return nil, err
	}

	rawVersion := cols.col(row, s.column("version"))
	rawEngine := cols.col(row, s.column("engine"))

	// Apply YAML-declared transforms. The carve-outs that used to
	// live as `if s.config.Type == "lambda"` etc. are now expressed
	// as named operations in resources.yaml; see pkg/config/transforms.go.
	// Order matters: version is computed first because the engine
	// transform's from_version_major op depends on the post-transform
	// version (OpenSearch's Elasticsearch-vs-OpenSearch detection).
	version, skip := applyVersionTransform(
		rawVersion,
		s.config.Transforms.Version,
		func(name string) string { return cols.col(row, name) },
	)
	if skip {
		return nil, nil
	}
	engine := applyEngineTransform(rawEngine, version, s.config.Transforms.Engine)

	// Parse tags to extract service. s.column("tags") returns "" when
	// YAML omits tags, in which case cols.col looks up the empty
	// column name (always "") and ParseTags handles "" as no-tags.
	tagsJSON := cols.col(row, s.column("tags"))
	tags, err := ParseTags(tagsJSON)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to parse tags",
			"resource_id", resourceID,
			"error", err)
		tags = nil
	}

	// Collect every non-typed YAML mapping value into Extra, keyed by
	// the YAML logical name. Empty strings still produce an entry so
	// downstream code can distinguish "configured but missing" from
	// "not configured at all". Both required and field mappings are
	// candidates — required vs optional is a load-time validation
	// concept, not a wire-shape one.
	var extra map[string]string
	for key, col := range s.allMappings() {
		if _, isWellKnown := wellKnownFieldMappingKeys[key]; isWellKnown {
			continue
		}
		if col == "" {
			continue
		}
		if extra == nil {
			extra = make(map[string]string)
		}
		value := cols.col(row, col)
		if s.config.ID == "opensearch" && key == "region" {
			value = normalizeOpenSearchRegion(resourceID, value)
		}
		extra[key] = value
	}

	// Service derivation: prefer the configured app tag; if none is
	// present, fall back to extracting a service name from Extra["name"]
	// (which is the cloud resource name when the user has mapped it).
	tagConfig := DefaultTagConfig()
	service := tagConfig.GetAppTag(tags)
	if service == "" {
		service = extractServiceFromName(extra["name"])
	}

	// Build resource
	discoveredAt := time.Now()
	if ctxTime, ok := ctx.Value(discoveredAtKey).(time.Time); ok {
		discoveredAt = ctxTime
	}

	resource := &types.Resource{
		ID:             resourceID,
		Type:           types.ResourceType(s.config.ID),
		CloudProvider:  s.CloudProvider(),
		CurrentVersion: version,
		Engine:         engine,
		Service:        service,
		Tags:           tags,
		Extra:          extra,
		DiscoveredAt:   discoveredAt,
	}

	return resource, nil
}

func normalizeOpenSearchRegion(resourceID, region string) string {
	if awsRegionPattern.MatchString(region) {
		return region
	}

	resourceARN, err := arn.Parse(resourceID)
	if err != nil || resourceARN.Service != "es" || !strings.HasPrefix(resourceARN.Resource, "domain/") ||
		!awsRegionPattern.MatchString(resourceARN.Region) {
		return region
	}

	return resourceARN.Region
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
