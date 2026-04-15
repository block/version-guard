package wiz

import (
	"context"
	"fmt"
	"log"

	"github.com/pkg/errors"

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
