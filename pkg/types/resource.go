package types

import "time"

// ResourceType represents the type of cloud resource. Production code
// uses YAML-declared config IDs (e.g. "aurora-mysql", "eks") as
// ResourceType values; the named constants below are retained only as
// fixture identifiers for tests in pkg/snapshot, pkg/store, and the
// detection/orchestrator workflows. Adding a new resource type is a
// resources.yaml change and does not require a constant here.
type ResourceType string

const (
	// ResourceTypeAurora is a fixture identifier used by tests.
	ResourceTypeAurora ResourceType = "AURORA"
	// ResourceTypeElastiCache is a fixture identifier used by tests.
	ResourceTypeElastiCache ResourceType = "ELASTICACHE"
	// ResourceTypeOpenSearch is a fixture identifier used by tests.
	ResourceTypeOpenSearch ResourceType = "OPENSEARCH"
	// ResourceTypeEKS is a fixture identifier used by tests.
	ResourceTypeEKS ResourceType = "EKS"
	// ResourceTypeLambda is a fixture identifier used by tests.
	ResourceTypeLambda ResourceType = "LAMBDA"
)

// String returns the string representation of the ResourceType
func (r ResourceType) String() string {
	return string(r)
}

// Resource represents a cloud infrastructure resource with version information.
//
// The typed surface intentionally covers only what the system itself needs:
//   - identity: ID, Type, CloudProvider
//   - EOL lookup keys: Engine, CurrentVersion
//   - service grouping: Service (derived from Tags)
//   - tags as a structural map (used by Service derivation)
//   - timestamps
//
// Everything else, including human-readable name, cloud account, and region,
// flows through Extra under its YAML logical name (e.g. Extra["name"],
// Extra["account_id"], Extra["region"]). This keeps the typed core minimal
// and makes new per-resource attributes a YAML-only change.
type Resource struct {
	// Tags are key-value pairs associated with the resource
	Tags map[string]string

	// Extra carries every YAML inventory.field_mappings value that is not
	// a typed field on Resource. The key is the YAML logical name
	// (e.g. "name", "account_id", "region", "owner") and the value is
	// the raw CSV cell. nil when the resource config defines no
	// non-typed fields.
	Extra map[string]string `json:",omitempty"`

	// DiscoveredAt is the timestamp when this resource was discovered
	DiscoveredAt time.Time

	// ID is the cloud-specific resource identifier (ARN for AWS, resource path for GCP, etc.)
	ID string

	// Service is the application or service name that owns this resource
	Service string

	// CurrentVersion is the engine or runtime version currently running
	CurrentVersion string

	// Engine is the database/cache/runtime engine type (e.g., "aurora-mysql", "postgres", "redis")
	Engine string

	// Type is the type of resource (Aurora, CloudSQL, etc.)
	Type ResourceType

	// CloudProvider is the cloud platform hosting this resource (AWS, GCP, Azure)
	CloudProvider CloudProvider
}

// VersionLifecycle represents the lifecycle information for a specific version
type VersionLifecycle struct {
	// ReleaseDate is when this version was released
	ReleaseDate *time.Time

	// EOLDate is when this version reaches End-of-Life
	EOLDate *time.Time

	// ExtendedSupportEnd is when extended support ends (if applicable)
	ExtendedSupportEnd *time.Time

	// DeprecationDate is when this version is deprecated
	DeprecationDate *time.Time

	// FetchedAt is the timestamp when this lifecycle data was fetched
	FetchedAt time.Time

	// Version is the version string (e.g., "5.6.10a", "7.0.1")
	Version string

	// Engine is the engine type (e.g., "aurora-mysql", "postgres")
	Engine string

	// Source indicates where this lifecycle data came from (e.g., "aws-rds-api", "endoflife.date")
	Source string

	// RecommendedVersion is the latest currently-supported cycle for
	// this product as reported by the EOL provider. Non-extended
	// support is preferred; if no non-extended cycle exists, the
	// provider falls back to the newest extended-support cycle so
	// the user still gets *some* concrete target. Used by the RED
	// path and the YELLOW approaching-EOL path. Empty when the
	// provider couldn't determine any supported cycle (404 product,
	// every cycle past EOL, etc.).
	RecommendedVersion string

	// RecommendedNonExtendedVersion is the latest cycle that is
	// supported AND NOT in (paid) extended support. Used by the
	// YELLOW extended-support recommendation path, where suggesting
	// another extended-support cycle would falsely claim the upgrade
	// avoids extended-support costs. Empty when every supported
	// cycle for this product is already in extended support — in
	// that case the policy layer falls back to a neutral message
	// rather than over-promising cost relief.
	RecommendedNonExtendedVersion string

	// IsEOL indicates if the version is past End-of-Life
	IsEOL bool

	// IsDeprecated indicates if the version is deprecated
	IsDeprecated bool

	// IsExtendedSupport indicates if the version is in extended support (typically higher cost)
	IsExtendedSupport bool

	// IsSupported indicates if the version is currently supported
	IsSupported bool
}

// Finding represents a detected version drift issue.
//
// The typed surface mirrors Resource: only fields the system itself
// requires (identity, EOL keys, service, classification metadata) are
// typed. Optional descriptive attributes — human-readable name, cloud
// account, region, and any YAML-defined extras — live in Extra under
// their YAML logical name. Wire-shape is locked by snapshot v2.
type Finding struct {
	// Tags are the resource's key-value metadata (e.g., AWS resource tags)
	Tags map[string]string `json:",omitempty"`

	// Extra carries every non-typed value the inventory layer collected
	// for this resource. Passed through verbatim from Resource.Extra so
	// downstream consumers (snapshots, dashboards) can read user-defined
	// attributes without a schema change.
	Extra map[string]string `json:",omitempty"`

	// EOLDate is when the current version reaches End-of-Life
	EOLDate *time.Time

	// DetectedAt is when this finding was first detected
	DetectedAt time.Time

	// UpdatedAt is when this finding was last updated
	UpdatedAt time.Time

	// ResourceID is the cloud-specific resource identifier
	ResourceID string

	// Service is the application or service name
	Service string

	// CurrentVersion is the version currently running
	CurrentVersion string

	// Engine is the engine type
	Engine string

	// Message is a human-readable description of the issue
	Message string

	// Recommendation is the recommended action to resolve the issue
	Recommendation string

	// ResourceType is the type of resource
	ResourceType ResourceType

	// CloudProvider is the cloud platform
	CloudProvider CloudProvider

	// Status is the compliance status (RED/YELLOW/GREEN/UNKNOWN)
	Status Status
}

// ScanSummary provides aggregate statistics from a scan
//
//nolint:govet // field alignment sacrificed for readability
type ScanSummary struct {
	// ScanStartTime is when the scan started
	ScanStartTime time.Time

	// ScanEndTime is when the scan completed
	ScanEndTime time.Time

	// CompliancePercentage is the percentage of resources that are compliant (GREEN)
	CompliancePercentage float64

	// TotalResources is the total number of resources scanned
	TotalResources int

	// RedCount is the number of resources with RED status
	RedCount int

	// YellowCount is the number of resources with YELLOW status
	YellowCount int

	// GreenCount is the number of resources with GREEN status
	GreenCount int

	// UnknownCount is the number of resources with UNKNOWN status
	UnknownCount int

	// ResourceType is the type of resources scanned
	ResourceType ResourceType

	// CloudProvider is the cloud provider scanned
	CloudProvider CloudProvider
}
