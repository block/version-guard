package config

import (
	"os"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"

	"github.com/block/Version-Guard/pkg/config/defaults"
	"github.com/block/Version-Guard/pkg/eol/endoflife"
)

// LoadResourcesConfig loads and parses the resources configuration.
//
// When path is empty, the canonical YAML embedded into the binary at
// build time is used (see pkg/config/defaults). This makes the binary
// self-contained: a default install scans the standard catalog without
// needing a sidecar config file.
//
// When path is non-empty, the file at that path fully replaces the
// embedded default — no merge, no overlay. Operators who want a
// different resource set, mappings, or EOL providers ship their own
// YAML and point CONFIG_PATH at it. The shape and validation rules
// are identical regardless of source.
func LoadResourcesConfig(path string) (*ResourcesConfig, error) {
	data, source, err := loadResourcesYAML(path)
	if err != nil {
		return nil, err
	}

	var config ResourcesConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, errors.Wrapf(err, "failed to parse YAML config from %s", source)
	}

	// Validate config
	if err := validateConfig(&config); err != nil {
		return nil, errors.Wrapf(err, "invalid configuration from %s", source)
	}

	return &config, nil
}

// loadResourcesYAML returns the raw YAML bytes plus a human-readable
// source label suitable for inclusion in error messages. Empty path =>
// embedded default; non-empty => disk read.
func loadResourcesYAML(path string) (data []byte, source string, err error) {
	if path == "" {
		return defaults.ResourcesYAML, "embedded default resources.yaml", nil
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed to read config file: %s", path)
	}
	return data, path, nil
}

// validateConfig validates the resources configuration
func validateConfig(config *ResourcesConfig) error {
	if config.Version == "" {
		return errors.New("version is required")
	}

	for i := range config.Resources {
		resource := &config.Resources[i]
		if resource.ID == "" {
			return errors.Errorf("resource[%d]: id is required", i)
		}
		if resource.Type == "" {
			return errors.Errorf("resource[%d]: type is required", i)
		}
		if resource.CloudProvider == "" {
			return errors.Errorf("resource[%d]: cloud_provider is required", i)
		}
		if resource.Inventory.Source == "" {
			return errors.Errorf("resource[%d]: inventory.source is required", i)
		}
		if resource.EOL.Provider == "" {
			return errors.Errorf("resource[%d]: eol.provider is required", i)
		}
		if resource.EOL.Product == "" {
			return errors.Errorf("resource[%d]: eol.product is required", i)
		}
		if err := validateEOLConfig(resource); err != nil {
			return errors.Wrapf(err, "resource[%d] %q", i, resource.ID)
		}
		if err := validateMappings(resource); err != nil {
			return errors.Wrapf(err, "resource[%d] %q", i, resource.ID)
		}
		if err := resource.Transforms.validate(); err != nil {
			return errors.Wrapf(err, "resource[%d] %q", i, resource.ID)
		}
	}

	return nil
}

func validateEOLConfig(resource *ResourceConfig) error {
	if resource.EOL.Schema == endoflife.SchemaDeclarative && resource.EOL.Lifecycle == nil {
		return errors.New("eol.lifecycle is required when eol.schema is declarative")
	}
	if resource.EOL.Lifecycle != nil {
		if resource.EOL.Schema == "" {
			resource.EOL.Schema = endoflife.SchemaDeclarative
		}
		if resource.EOL.Schema != endoflife.SchemaDeclarative {
			return errors.New("eol.lifecycle requires eol.schema to be declarative")
		}
		if err := endoflife.ValidateDeclarativeLifecycleConfig(resource.EOL.Lifecycle); err != nil {
			return errors.Wrap(err, "invalid eol.lifecycle")
		}
		return nil
	}
	if _, err := endoflife.GetSchemaAdapter(defaultSchema(resource.EOL.Schema)); err != nil {
		return err
	}
	return nil
}

func defaultSchema(schema string) string {
	if schema == "" {
		return endoflife.SchemaStandard
	}
	return schema
}

// validateMappings enforces three rules on a resource's
// required_mappings / field_mappings split:
//
//  1. resource_id MUST appear in required_mappings — the system
//     can't function without a primary key for each row.
//  2. Every value in required_mappings must be non-empty. Listing a
//     key in required_mappings is the YAML way of asserting it is
//     mandatory; an empty string means the user forgot to fill it in.
//  3. A key may appear in required_mappings OR field_mappings but
//     never both. Duplicates indicate a copy-paste mistake and would
//     hide intent from the next reader.
//
// What's "required" is per-resource and self-declared in YAML. We
// no longer carry a Go-side switch on resource.Type because the
// previous carve-outs (lambda derives version/engine implicitly from
// graphEntity.properties; eks defaults engine to "eks"; opensearch
// derives engine from the version) are simply expressed as different
// required_mappings sets in the config file.
func validateMappings(resource *ResourceConfig) error {
	inv := &resource.Inventory

	id, ok := inv.RequiredMappings["resource_id"]
	if !ok || id == "" {
		return errors.New("inventory.required_mappings.resource_id is required")
	}

	for key, val := range inv.RequiredMappings {
		if val == "" {
			return errors.Errorf("inventory.required_mappings.%s must not be empty", key)
		}
	}

	for key := range inv.RequiredMappings {
		if _, dup := inv.FieldMappings[key]; dup {
			return errors.Errorf(
				"mapping %q appears in both inventory.required_mappings and inventory.field_mappings; pick one",
				key,
			)
		}
	}

	return nil
}
