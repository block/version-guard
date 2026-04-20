package wiz

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/pkg/errors"

	"github.com/block/Version-Guard/pkg/types"
)

// columnIndex maps CSV header names to their column positions.
// Built from the header row of a Wiz saved report CSV.
type columnIndex map[string]int

// columnAliases maps canonical column names to alternative names found in
// different Wiz report schemas. When a canonical name is not present in the
// CSV header, the alias is tried as a fallback. This handles schema
// differences such as the DB_SERVER schema used by OpenSearch reports.
var columnAliases = map[string]string{
	"versionDetails.version":  "version",
	"region":                  "regionLocation",
	"cloudAccount.externalId": "cloudPlatform",
	"name":                    "Name",
}

// buildColumnIndex creates a columnIndex from a CSV header row.
// It also registers unprefixed aliases for columns with dotted prefixes
// (e.g., "DB_SERVER.externalId" is also stored as "externalId") so that
// field mappings work across different Wiz report schemas.
func buildColumnIndex(header []string) columnIndex {
	idx := make(columnIndex, len(header)*2)
	for i, name := range header {
		idx[name] = i
		// Strip prefix: "DB_SERVER.externalId" → "externalId"
		if dotIdx := strings.LastIndex(name, "."); dotIdx >= 0 {
			unprefixed := name[dotIdx+1:]
			if _, exists := idx[unprefixed]; !exists {
				idx[unprefixed] = i
			}
		}
	}
	return idx
}

// col returns the value of the named column from a CSV row.
// Returns "" if the column is not in the header or the row is too short.
// Falls back to columnAliases if the canonical name is not found.
func (ci columnIndex) col(row []string, name string) string {
	i, ok := ci[name]
	if !ok {
		// Try alias fallback
		if alias, hasAlias := columnAliases[name]; hasAlias {
			i, ok = ci[alias]
		}
		if !ok {
			return ""
		}
	}
	if i >= len(row) {
		return ""
	}
	return row[i]
}

// hasColumn returns true if the named column (or an alias for it) exists in the index.
func (ci columnIndex) hasColumn(name string) bool {
	if _, ok := ci[name]; ok {
		return true
	}
	if alias, hasAlias := columnAliases[name]; hasAlias {
		if _, ok := ci[alias]; ok {
			return true
		}
	}
	return false
}

// require returns the value of the named column, or an error if it is missing
// from the header or empty in the row.
func (ci columnIndex) require(row []string, name string) (string, error) {
	v := ci.col(row, name)
	if v == "" {
		if !ci.hasColumn(name) {
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
	logger *slog.Logger,
) ([]*types.Resource, error) {
	if logger == nil {
		logger = slog.Default()
	}
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

	// Validate that all required columns are present (using alias-aware lookup)
	for _, name := range requiredColumns {
		if !cols.hasColumn(name) {
			return nil, fmt.Errorf("required column %q not found in CSV header (have: %v)", name, rows[0])
		}
	}

	totalDataRows := len(rows) - 1
	logger.InfoContext(ctx, "processing Wiz report",
		"total_rows", totalDataRows,
		"report_id", reportID)

	// Parse data rows (skip header)
	var resources []*types.Resource
	var filteredCount int
	var filteredNativeTypes []string
	for i, row := range rows[1:] {
		// Apply resource type filter
		if !filterRow(cols, row) {
			filteredCount++
			if len(filteredNativeTypes) < 3 {
				if nt := cols.col(row, colHeaderNativeType); nt != "" {
					filteredNativeTypes = append(filteredNativeTypes, nt)
				}
			}
			continue
		}

		// Parse the row
		resource, err := parseRow(ctx, cols, row)
		if err != nil {
			// Log error but continue processing other rows
			logger.WarnContext(ctx, "failed to parse resource from CSV row",
				"row_number", i+1,
				"error", err)
			continue
		}

		if resource != nil {
			resources = append(resources, resource)
		}
	}

	logger.InfoContext(ctx, "Wiz report processing complete",
		"matched", len(resources),
		"filtered", filteredCount,
		"total_rows", totalDataRows,
		"sample_filtered_types", filteredNativeTypes)

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
