package config

import "github.com/block/Version-Guard/pkg/eol/endoflife"

// ResourcesConfig represents the root configuration structure
type ResourcesConfig struct {
	Version   string           `yaml:"version"`
	Resources []ResourceConfig `yaml:"resources"`
}

// ResourceConfig defines configuration for a single resource type.
//
//nolint:govet // YAML field order chosen for readability of resources.yaml
type ResourceConfig struct {
	ID            string          `yaml:"id"`
	Type          string          `yaml:"type"`
	CloudProvider string          `yaml:"cloud_provider"`
	Inventory     InventoryConfig `yaml:"inventory"`
	EOL           EOLConfig       `yaml:"eol"`

	// Transforms declares per-resource version/engine reshaping.
	// Optional — resources without quirky inventory data omit it
	// entirely and the parser uses raw column values. See
	// transforms.go for the operation set and rationale.
	Transforms TransformsConfig `yaml:"transforms,omitempty"`
}

// InventoryConfig defines inventory source configuration.
//
// Mappings are split into two YAML maps for clarity:
//
//   - RequiredMappings  — every entry MUST be present and non-empty;
//     validated at config load time so YAML typos fail fast at startup
//     instead of mid-scan. Each resource self-declares its required set
//     so we don't carry hardcoded per-resource-type carve-outs in Go.
//
//   - FieldMappings     — optional mappings. Missing values produce
//     empty strings on the typed Resource (for typed keys) or absent
//     entries in Resource.Extra (for non-typed keys).
//
// A given key MUST appear in at most one of the two maps; the loader
// rejects duplicates. The split is purely a UX/documentation aid for
// people reading the YAML — the parser reads the union of both.
type InventoryConfig struct {
	RequiredMappings  map[string]string `yaml:"required_mappings"`
	FieldMappings     map[string]string `yaml:"field_mappings"`
	Source            string            `yaml:"source"`
	NativeTypePattern string            `yaml:"native_type_pattern"`
}

// EOLConfig defines EOL provider configuration
type EOLConfig struct {
	Provider  string                                `yaml:"provider"`
	Product   string                                `yaml:"product"`
	Schema    string                                `yaml:"schema"`
	Lifecycle *endoflife.DeclarativeLifecycleConfig `yaml:"lifecycle,omitempty"`
}
