package eol

import (
	"context"

	"github.com/block/Version-Guard/pkg/types"
)

// Provider defines the interface for fetching version lifecycle (EOL) data
type Provider interface {
	// GetVersionLifecycle retrieves lifecycle information for a specific engine version.
	// Implementations may return a non-nil diagnostic lifecycle with a non-nil error;
	// callers should preserve that lifecycle when reporting the failure.
	GetVersionLifecycle(ctx context.Context, engine, version string) (*types.VersionLifecycle, error)

	// ListAllVersions retrieves all known versions for an engine
	ListAllVersions(ctx context.Context, engine string) ([]*types.VersionLifecycle, error)

	// Name returns the name of this EOL provider (e.g., "aws-rds-api", "endoflife.date")
	Name() string

	// Engines returns the list of engines this provider supports
	Engines() []string
}
