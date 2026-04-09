package emitters

import (
	"context"

	"github.com/block/Version-Guard/pkg/types"
)

// IssueTrackerEmitter emits findings to an issue tracking system (e.g., Jira, ServiceNow, Linear)
type IssueTrackerEmitter interface {
	// Emit creates or updates issues for the given findings
	Emit(ctx context.Context, snapshotID string, findings []*types.Finding) (*IssueTrackerResult, error)
}

// IssueTrackerResult contains the result of emitting to an issue tracker
type IssueTrackerResult struct {
	IssuesCreated int
	IssuesUpdated int
	IssuesClosed  int
}

// DashboardEmitter pushes compliance data to a dashboard or scorecard system
type DashboardEmitter interface {
	// Emit pushes summary statistics to the dashboard
	Emit(ctx context.Context, snapshotID string, summary *types.SnapshotSummary) (*DashboardResult, error)
}

// DashboardResult contains the result of emitting to a dashboard
type DashboardResult struct {
	ServicesUpdated int
}
