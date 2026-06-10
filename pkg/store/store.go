package store

import (
	"context"
	"time"

	"github.com/block/Version-Guard/pkg/types"
)

// Store defines the interface for persisting and retrieving findings
type Store interface {
	// ClearFindings removes all findings from the store.
	ClearFindings(ctx context.Context) error

	// SaveFindings saves or updates findings
	SaveFindings(ctx context.Context, findings []*types.Finding) error

	// GetFinding retrieves a specific finding by resource ID
	GetFinding(ctx context.Context, resourceID string) (*types.Finding, error)

	// ListFindings retrieves findings with optional filtering
	ListFindings(ctx context.Context, filters FindingFilters) ([]*types.Finding, error)

	// GetSummary calculates aggregate statistics for findings
	GetSummary(ctx context.Context, filters FindingFilters) (*ScanSummary, error)

	// DeleteStaleFindings removes findings older than the specified time
	DeleteStaleFindings(ctx context.Context, resourceType types.ResourceType, olderThan time.Time) error
}

// FindingFilters defines optional filters for querying findings.
//
// Filterable attributes are limited to the typed Finding surface.
// Account ID and region moved into Finding.Extra in snapshot v2+ and
// are no longer filterable through this interface; consumers that
// need to slice on those should iterate findings and read Extra
// directly.
type FindingFilters struct {
	ResourceType *types.ResourceType
	Service      *string
	Status       *types.Status
	Engine       *string
}

// ScanSummary provides aggregate statistics for a scan
//
//nolint:govet // field alignment sacrificed for logical grouping
type ScanSummary struct {
	TotalResources       int
	RedCount             int
	YellowCount          int
	GreenCount           int
	UnknownCount         int
	CompliancePercentage float64 // (GreenCount / TotalResources) * 100
	LastScanTime         time.Time
}
