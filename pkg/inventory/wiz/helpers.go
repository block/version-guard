package wiz

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/pkg/errors"

	"github.com/block/Version-Guard/pkg/registry"
	"github.com/block/Version-Guard/pkg/types"
)

// columnIndex maps CSV header names to their column positions.
// Built from the header row of a Wiz saved report CSV.
type columnIndex map[string]int

// buildColumnIndex creates a columnIndex from a CSV header row.
func buildColumnIndex(header []string) columnIndex {
	idx := make(columnIndex, len(header))
	for i, name := range header {
		idx[name] = i
	}
	return idx
}

// col returns the value of the named column from a CSV row.
// Returns "" if the column is not in the header or the row is too short.
func (ci columnIndex) col(row []string, name string) string {
	i, ok := ci[name]
	if !ok || i >= len(row) {
		return ""
	}
	return row[i]
}

// require returns the value of the named column, or an error if it is missing
// from the header or empty in the row.
func (ci columnIndex) require(row []string, name string) (string, error) {
	v := ci.col(row, name)
	if v == "" {
		if _, ok := ci[name]; !ok {
			return "", fmt.Errorf("column %q not found in CSV header", name)
		}
		return "", fmt.Errorf("missing value for column %q", name)
	}
	return v, nil
}

// Wiz CSV column header names used across all inventory sources.
const (
	colHeaderExternalID = "externalId"
	colHeaderName       = "name"
	colHeaderNativeType = "nativeType"
	colHeaderAccountID  = "cloudAccount.externalId"
	colHeaderVersion    = "versionDetails.version"
	colHeaderRegion     = "region"
	colHeaderTags       = "tags"
	colHeaderEngineKind = "typeFields.kind"
)

// rowFilterFunc decides whether a CSV row should be processed.
// Returns true if the row should be parsed, false to skip it.
type rowFilterFunc func(cols columnIndex, row []string) bool

// rowParserFunc parses a CSV row into a Resource.
// Returns nil resource to skip, non-nil to include in results.
type rowParserFunc func(ctx context.Context, cols columnIndex, row []string) (*types.Resource, error)

// parseWizReport is a shared helper that implements the common CSV-row-iteration pattern
// used by Aurora, EKS, and ElastiCache inventory sources.
//
// It reads the header row to build a dynamic column index, then iterates over
// data rows, applying the filter and parser functions.
//
// Parameters:
//   - ctx: Context for the operation
//   - client: Wiz client for fetching report data
//   - reportID: The Wiz report ID to fetch
//   - requiredColumns: Column header names that must be present in the CSV
//   - filterRow: Function to filter rows (e.g., check nativeType)
//   - parseRow: Function to parse a valid row into a Resource
//
// Returns:
//   - List of successfully parsed resources
//   - Error if report fetching fails (not if individual rows fail to parse)
func parseWizReport(
	ctx context.Context,
	client *Client,
	reportID string,
	requiredColumns []string,
	filterRow rowFilterFunc,
	parseRow rowParserFunc,
) ([]*types.Resource, error) {
	// Fetch report data
	rows, err := client.GetReportData(ctx, reportID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch Wiz report data")
	}

	if len(rows) < 2 {
		// Empty report (only header row or completely empty)
		return []*types.Resource{}, nil
	}

	// Build column index from header row
	cols := buildColumnIndex(rows[0])

	// Validate that all required columns are present
	for _, name := range requiredColumns {
		if _, ok := cols[name]; !ok {
			return nil, fmt.Errorf("required column %q not found in CSV header (have: %v)", name, rows[0])
		}
	}

	// Parse data rows (skip header)
	var resources []*types.Resource
	for i, row := range rows[1:] {
		// Apply resource type filter
		if !filterRow(cols, row) {
			continue
		}

		// Parse the row
		resource, err := parseRow(ctx, cols, row)
		if err != nil {
			// Log error but continue processing other rows
			// TODO: wire through proper structured logger (e.g., *slog.Logger)
			log.Printf("WARN: row %d: failed to parse resource: %v", i+1, err)
			continue
		}

		if resource != nil {
			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// extractServiceFromName attempts to extract service name from resource name.
// Used as a fallback when service tags and registry lookups are unavailable.
//
// Example transformations:
//   - "my-service-prod-cluster" → "my-service"
//   - "payment-api-staging-db" → "payment-api"
//   - "prod-cache" → "prod-cache" (no suffix found)
//
// Strategy: Split on hyphens and remove common environment/resource suffixes
// from the end, then rejoin the remaining parts.
func extractServiceFromName(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) == 0 {
		return name
	}

	// Scan from the end and find the first non-suffix part
	// This handles cases like "cache-prod-valkey" correctly
	endIdx := len(parts)
	for i := len(parts) - 1; i >= 0; i-- {
		if isEnvironmentSuffix(parts[i]) || isCommonSuffix(parts[i]) {
			endIdx = i
		} else {
			// Found a non-suffix part, stop scanning
			break
		}
	}

	// If we removed everything or nothing meaningful remains, return original
	if endIdx == 0 || endIdx == len(parts) {
		return name
	}

	return strings.Join(parts[:endIdx], "-")
}

// isEnvironmentSuffix checks if a string is a common environment suffix.
// Used by extractServiceFromName to identify non-service parts of resource names.
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

// isCommonSuffix checks if a string is a common AWS resource type suffix.
// Used by extractServiceFromName to identify non-service parts of resource names.
func isCommonSuffix(s string) bool {
	s = strings.ToLower(s)
	suffixes := []string{"cluster", "db", "database", "rds", "aurora", "cache", "redis", "memcached", "valkey"}
	for _, suffix := range suffixes {
		if s == suffix {
			return true
		}
	}
	return false
}

// parseAWSResourceRow is a shared parser for Aurora and ElastiCache CSV rows.
// Both resource types have identical parsing logic, differing only in the
// engine normalizer function and the resource type constant.
func parseAWSResourceRow(
	ctx context.Context,
	cols columnIndex,
	row []string,
	resourceType types.ResourceType,
	normalizeEngine func(string) string,
	tagConfig *TagConfig,
	registryClient registry.Client,
) (*types.Resource, error) {
	resourceARN, err := cols.require(row, colHeaderExternalID)
	if err != nil {
		return nil, fmt.Errorf("missing ARN")
	}

	parsedARN, err := arn.Parse(resourceARN)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid ARN: %s", resourceARN)
	}

	resourceName := cols.col(row, colHeaderName)
	accountID := cols.col(row, colHeaderAccountID)
	if accountID == "" {
		accountID = parsedARN.AccountID
	}

	engine := normalizeEngine(cols.col(row, colHeaderEngineKind))
	version := cols.col(row, colHeaderVersion)
	region := cols.col(row, colHeaderRegion)

	tagsJSON := cols.col(row, colHeaderTags)
	tags, err := ParseTags(tagsJSON)
	if err != nil {
		tags = make(map[string]string)
	}

	service := tagConfig.GetAppTag(tags)
	if service == "" {
		if registryClient != nil {
			if serviceInfo, err := registryClient.GetServiceByAWSAccount(ctx, accountID, region); err == nil {
				service = serviceInfo.ServiceName
			}
		}
		if service == "" {
			service = extractServiceFromName(resourceName)
		}
	}

	brand := tagConfig.GetBrandTag(tags)

	return &types.Resource{
		ID:             resourceARN,
		Name:           resourceName,
		Type:           resourceType,
		CloudProvider:  types.CloudProviderAWS,
		Service:        service,
		CloudAccountID: accountID,
		CloudRegion:    region,
		Brand:          brand,
		CurrentVersion: version,
		Engine:         engine,
		Tags:           tags,
		DiscoveredAt:   time.Now(),
	}, nil
}
