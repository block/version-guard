package policy

import "github.com/block/Version-Guard/pkg/types"

// VersionPolicy defines the interface for version drift classification policies
type VersionPolicy interface {
	// Classify determines the compliance status (RED/YELLOW/GREEN) for a resource
	// based on its version lifecycle information
	Classify(resource *types.Resource, lifecycle *types.VersionLifecycle) types.Status

	// GetMessage generates a human-readable message describing the status
	GetMessage(resource *types.Resource, lifecycle *types.VersionLifecycle, status types.Status) string

	// Name returns the name of this policy
	Name() string
}
