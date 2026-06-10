package memory

import (
	"context"
	"sync"
	"time"

	"github.com/block/Version-Guard/pkg/store"
	"github.com/block/Version-Guard/pkg/types"
)

// Store is an in-memory implementation of the Store interface
// Thread-safe with mutex protection
//
//nolint:govet // field alignment sacrificed for logical grouping
type Store struct {
	mu       sync.RWMutex
	findings map[string]*types.Finding // key: resourceID
}

// NewStore creates a new in-memory store
func NewStore() *Store {
	return &Store{
		findings: make(map[string]*types.Finding),
	}
}

// ClearFindings removes all findings from memory.
func (s *Store) ClearFindings(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.findings = make(map[string]*types.Finding)
	return nil
}

// SaveFindings saves or updates findings in memory
func (s *Store) SaveFindings(ctx context.Context, findings []*types.Finding) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, finding := range findings {
		// Set update timestamp
		finding.UpdatedAt = now
		if finding.DetectedAt.IsZero() {
			finding.DetectedAt = now
		}

		// Store by resource ID
		s.findings[finding.ResourceID] = finding
	}

	return nil
}

// GetFinding retrieves a specific finding by resource ID
func (s *Store) GetFinding(ctx context.Context, resourceID string) (*types.Finding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	finding, exists := s.findings[resourceID]
	if !exists {
		return nil, nil // Not found, return nil without error
	}

	return finding, nil
}

// ListFindings retrieves findings with optional filtering
func (s *Store) ListFindings(ctx context.Context, filters store.FindingFilters) ([]*types.Finding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*types.Finding
	for _, finding := range s.findings {
		if matchesFilters(finding, filters) {
			results = append(results, finding)
		}
	}

	return results, nil
}

// GetSummary calculates aggregate statistics for findings
func (s *Store) GetSummary(ctx context.Context, filters store.FindingFilters) (*store.ScanSummary, error) {
	findings, err := s.ListFindings(ctx, filters)
	if err != nil {
		return nil, err
	}

	summary := &store.ScanSummary{
		TotalResources: len(findings),
	}

	var lastScanTime time.Time
	for _, f := range findings {
		// Count by status
		switch f.Status {
		case types.StatusRed:
			summary.RedCount++
		case types.StatusYellow:
			summary.YellowCount++
		case types.StatusGreen:
			summary.GreenCount++
		case types.StatusUnknown:
			summary.UnknownCount++
		}

		// Track latest scan time
		if f.DetectedAt.After(lastScanTime) {
			lastScanTime = f.DetectedAt
		}
	}

	summary.LastScanTime = lastScanTime

	// Calculate compliance percentage (green / total)
	if summary.TotalResources > 0 {
		summary.CompliancePercentage = (float64(summary.GreenCount) / float64(summary.TotalResources)) * 100
	}

	return summary, nil
}

// DeleteStaleFindings removes findings older than the specified time
func (s *Store) DeleteStaleFindings(ctx context.Context, resourceType types.ResourceType, olderThan time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, finding := range s.findings {
		if finding.ResourceType == resourceType && finding.UpdatedAt.Before(olderThan) {
			delete(s.findings, id)
		}
	}

	return nil
}

// matchesFilters checks if a finding matches the given filters
func matchesFilters(finding *types.Finding, filters store.FindingFilters) bool {
	if filters.ResourceType != nil && finding.ResourceType != *filters.ResourceType {
		return false
	}

	if filters.Service != nil && finding.Service != *filters.Service {
		return false
	}

	if filters.Status != nil && finding.Status != *filters.Status {
		return false
	}

	if filters.Engine != nil && finding.Engine != *filters.Engine {
		return false
	}

	return true
}
