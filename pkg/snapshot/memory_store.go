package snapshot

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/block/Version-Guard/pkg/types"
)

// MemoryStore is an in-memory implementation of Store, intended for local
// development and tests. It is goroutine-safe but obviously not durable —
// snapshots disappear on process restart.
//
// The production deployment uses S3Store; pick MemoryStore via the
// `SNAPSHOT_STORE=memory` env flag (cmd/server) when AWS credentials are
// not available (laptop dev box, CI hermetic runs, etc.).
//
//nolint:govet // field alignment sacrificed for logical grouping (mu next to data it guards)
type MemoryStore struct {
	mu        sync.RWMutex
	snapshots map[string]*types.Snapshot
	order     []string // insertion order, most-recent last
}

// NewMemoryStore creates an empty in-process snapshot store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		snapshots: make(map[string]*types.Snapshot),
	}
}

// SaveSnapshot stores the snapshot under its SnapshotID.
func (m *MemoryStore) SaveSnapshot(_ context.Context, s *types.Snapshot) error {
	if s == nil {
		return fmt.Errorf("memory store: snapshot is nil")
	}
	if s.SnapshotID == "" {
		return fmt.Errorf("memory store: snapshot has empty SnapshotID")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.snapshots[s.SnapshotID]; !exists {
		m.order = append(m.order, s.SnapshotID)
	}
	m.snapshots[s.SnapshotID] = s
	return nil
}

// GetLatestSnapshot returns the most recently saved snapshot.
func (m *MemoryStore) GetLatestSnapshot(_ context.Context) (*types.Snapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.order) == 0 {
		return nil, fmt.Errorf("memory store: no snapshots available")
	}
	return m.snapshots[m.order[len(m.order)-1]], nil
}

// GetSnapshot returns a snapshot by ID.
func (m *MemoryStore) GetSnapshot(_ context.Context, snapshotID string) (*types.Snapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.snapshots[snapshotID]
	if !ok {
		return nil, fmt.Errorf("memory store: snapshot %q not found", snapshotID)
	}
	return s, nil
}

// ListSnapshots returns metadata for stored snapshots, most-recent first.
// limit <= 0 means "all".
func (m *MemoryStore) ListSnapshots(_ context.Context, limit int) ([]*Metadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*Metadata, 0, len(m.order))
	for _, id := range m.order {
		s := m.snapshots[id]
		out = append(out, &Metadata{
			SnapshotID:           s.SnapshotID,
			GeneratedAt:          s.GeneratedAt,
			TotalResources:       s.Summary.TotalResources,
			CompliancePercentage: s.Summary.CompliancePercentage,
			S3Key:                "memory://" + s.SnapshotID,
		})
	}
	// Most-recent first
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].GeneratedAt.After(out[j].GeneratedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
