package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadResourcesConfig_Success(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "resources.yaml")

	configContent := `version: v1
resources:
  - id: aurora-postgresql
    type: aurora
    cloud_provider: aws
    inventory:
      source: wiz
      native_type_pattern: "rds/AmazonAuroraPostgreSQL/cluster"
      required_mappings:
        resource_id: "externalId"
        version: "versionDetails.version"
        engine: "typeFields.kind"
      field_mappings:
        name: "name"
        account_id: "cloudAccount.externalId"
        region: "region"
    eol:
      provider: endoflife-date
      product: amazon-aurora-postgresql
      schema: standard
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Load the config
	cfg, err := LoadResourcesConfig(configFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify config structure
	assert.Equal(t, "v1", cfg.Version)
	assert.Len(t, cfg.Resources, 1)

	// Verify resource details
	res := cfg.Resources[0]
	assert.Equal(t, "aurora-postgresql", res.ID)
	assert.Equal(t, "aurora", res.Type)
	assert.Equal(t, "aws", res.CloudProvider)

	// Verify inventory config
	assert.Equal(t, "wiz", res.Inventory.Source)
	assert.Equal(t, "rds/AmazonAuroraPostgreSQL/cluster", res.Inventory.NativeTypePattern)
	assert.Len(t, res.Inventory.RequiredMappings, 3)
	assert.Equal(t, "externalId", res.Inventory.RequiredMappings["resource_id"])
	assert.Equal(t, "typeFields.kind", res.Inventory.RequiredMappings["engine"])
	assert.Equal(t, "versionDetails.version", res.Inventory.RequiredMappings["version"])
	assert.Len(t, res.Inventory.FieldMappings, 3)
	assert.Equal(t, "name", res.Inventory.FieldMappings["name"])

	// Verify EOL config
	assert.Equal(t, "endoflife-date", res.EOL.Provider)
	assert.Equal(t, "amazon-aurora-postgresql", res.EOL.Product)
	assert.Equal(t, "standard", res.EOL.Schema)
}

func TestLoadResourcesConfig_MultipleResources(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "resources.yaml")

	configContent := `version: v1
resources:
  - id: aurora-postgresql
    type: aurora
    cloud_provider: aws
    inventory:
      source: wiz
      native_type_pattern: "rds/AmazonAuroraPostgreSQL/cluster"
      required_mappings:
        resource_id: "externalId"
        version: "versionDetails.version"
        engine: "typeFields.kind"
    eol:
      provider: endoflife-date
      product: amazon-aurora-postgresql
      schema: standard
  - id: eks
    type: eks
    cloud_provider: aws
    inventory:
      source: wiz
      native_type_pattern: "eks/Cluster"
      required_mappings:
        resource_id: "providerUniqueId"
        version: "versionDetails.version"
    eol:
      provider: endoflife-date
      product: amazon-eks
      schema: declarative
      lifecycle:
        deprecation_date:
          field: eol
        extended_support_end:
          field: extendedSupport
          bool_true_fallback: eol
        eol_date:
          field: extendedSupport
        deprecated_window: extended_support
        past_extended_support: unsupported
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := LoadResourcesConfig(configFile)
	require.NoError(t, err)
	assert.Len(t, cfg.Resources, 2)

	// Verify both resources
	assert.Equal(t, "aurora-postgresql", cfg.Resources[0].ID)
	assert.Equal(t, "eks", cfg.Resources[1].ID)
	assert.Equal(t, "standard", cfg.Resources[0].EOL.Schema)
	assert.Equal(t, "declarative", cfg.Resources[1].EOL.Schema)
	require.NotNil(t, cfg.Resources[1].EOL.Lifecycle)
	assert.Equal(t, "eol", cfg.Resources[1].EOL.Lifecycle.DeprecationDate.Field)
	assert.Equal(t, "extendedSupport", cfg.Resources[1].EOL.Lifecycle.EOLDate.Field)
}

// TestLoadResourcesConfig_EmbeddedDefault asserts the binary works
// out of the box: an empty path falls back to the canonical YAML
// embedded into pkg/config/defaults at build time. We don't pin the
// exact resource set (that's what `config/resources.yaml` content
// tests would do); we just assert that the embedded payload exists,
// parses, validates, and produces a non-empty catalog. If someone
// breaks the //go:embed directive or ships an empty file, this test
// fails before any docker-compose verification can.
func TestLoadResourcesConfig_EmbeddedDefault(t *testing.T) {
	cfg, err := LoadResourcesConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotEmpty(t, cfg.Version, "embedded YAML must declare a version")
	assert.NotEmpty(t, cfg.Resources, "embedded YAML must define at least one resource")

	// Spot-check that resource_id is set on every embedded resource —
	// the loader's validateMappings rejects missing values, so reaching
	// here means each entry passed validation. We assert it explicitly
	// to make any future weakening of the validator visible at this
	// test boundary.
	for i := range cfg.Resources {
		r := cfg.Resources[i]
		assert.NotEmpty(t, r.ID, "resource[%d] missing id", i)
		assert.NotEmpty(t, r.Inventory.RequiredMappings["resource_id"],
			"resource %q missing required_mappings.resource_id", r.ID)
	}

	byID := make(map[string]ResourceConfig, len(cfg.Resources))
	for _, r := range cfg.Resources {
		byID[r.ID] = r
	}

	eks := byID["eks"]
	require.NotNil(t, eks.EOL.Lifecycle)
	assert.Equal(t, "extendedSupport", eks.EOL.Lifecycle.EOLDate.Field,
		"EKS eol_date must stay on extendedSupport so the terminal EOL is end of extended support")

	lambda := byID["lambda"]
	require.NotNil(t, lambda.EOL.Lifecycle)
	assert.Equal(t, "support", lambda.EOL.Lifecycle.EOLDate.Field,
		"Lambda eol_date must stay on support so the actionable runtime EOL drives RED status")
	assert.Equal(t, "eol", lambda.EOL.Lifecycle.DeprecatedSupportEnd.Field,
		"Lambda deprecated_support_end should preserve AWS's later terminal date")
}

// TestLoadResourcesConfig_OverridePath asserts the override semantic:
// when CONFIG_PATH points at a real file, that file fully replaces
// the embedded default. We hand the loader a deliberately *smaller*
// catalog (one resource) and verify both that it loaded and that the
// embedded default's larger catalog was not silently merged in. This
// is the contract the user relies on to ship a custom resource set
// without rebuilding the binary.
func TestLoadResourcesConfig_OverridePath(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "custom.yaml")

	// Single-resource override — intentionally unlike the embedded
	// default so a merge regression would show up as len > 1.
	customContent := `version: v1
resources:
  - id: my-only-resource
    type: aurora
    cloud_provider: aws
    inventory:
      source: wiz
      native_type_pattern: "rds/AmazonAuroraPostgreSQL/cluster"
      required_mappings:
        resource_id: "externalId"
        version: "versionDetails.version"
        engine: "typeFields.kind"
    eol:
      provider: endoflife-date
      product: amazon-aurora-postgresql
      schema: standard
`
	require.NoError(t, os.WriteFile(configFile, []byte(customContent), 0644))

	cfg, err := LoadResourcesConfig(configFile)
	require.NoError(t, err)
	require.Len(t, cfg.Resources, 1, "override must fully replace embedded default, not merge")
	assert.Equal(t, "my-only-resource", cfg.Resources[0].ID)
}

func TestLoadResourcesConfig_FileNotFound(t *testing.T) {
	cfg, err := LoadResourcesConfig("/nonexistent/file.yaml")
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoadResourcesConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "invalid.yaml")

	invalidContent := `version: v1
resources:
  - id: test
    invalid yaml here [[[
`

	err := os.WriteFile(configFile, []byte(invalidContent), 0644)
	require.NoError(t, err)

	cfg, err := LoadResourcesConfig(configFile)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to parse YAML config")
}

func TestValidateConfig_MissingVersion(t *testing.T) {
	cfg := &ResourcesConfig{
		Version: "",
		Resources: []ResourceConfig{
			{
				ID:            "test",
				Type:          "aurora",
				CloudProvider: "aws",
				Inventory: InventoryConfig{
					Source: "wiz",
				},
				EOL: EOLConfig{
					Provider: "endoflife-date",
					Product:  "test",
				},
			},
		},
	}

	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "version is required")
}

func TestValidateConfig_MissingResourceID(t *testing.T) {
	cfg := &ResourcesConfig{
		Version: "v1",
		Resources: []ResourceConfig{
			{
				ID:            "", // Missing
				Type:          "aurora",
				CloudProvider: "aws",
				Inventory: InventoryConfig{
					Source: "wiz",
				},
				EOL: EOLConfig{
					Provider: "endoflife-date",
					Product:  "test",
				},
			},
		},
	}

	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "id is required")
}

func TestValidateConfig_MissingType(t *testing.T) {
	cfg := &ResourcesConfig{
		Version: "v1",
		Resources: []ResourceConfig{
			{
				ID:            "test",
				Type:          "", // Missing
				CloudProvider: "aws",
				Inventory: InventoryConfig{
					Source: "wiz",
				},
				EOL: EOLConfig{
					Provider: "endoflife-date",
					Product:  "test",
				},
			},
		},
	}

	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "type is required")
}

func TestValidateConfig_MissingCloudProvider(t *testing.T) {
	cfg := &ResourcesConfig{
		Version: "v1",
		Resources: []ResourceConfig{
			{
				ID:            "test",
				Type:          "aurora",
				CloudProvider: "", // Missing
				Inventory: InventoryConfig{
					Source: "wiz",
				},
				EOL: EOLConfig{
					Provider: "endoflife-date",
					Product:  "test",
				},
			},
		},
	}

	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cloud_provider is required")
}

func TestValidateConfig_MissingInventorySource(t *testing.T) {
	cfg := &ResourcesConfig{
		Version: "v1",
		Resources: []ResourceConfig{
			{
				ID:            "test",
				Type:          "aurora",
				CloudProvider: "aws",
				Inventory: InventoryConfig{
					Source: "", // Missing
				},
				EOL: EOLConfig{
					Provider: "endoflife-date",
					Product:  "test",
				},
			},
		},
	}

	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "inventory.source is required")
}

func TestValidateConfig_MissingEOLProvider(t *testing.T) {
	cfg := &ResourcesConfig{
		Version: "v1",
		Resources: []ResourceConfig{
			{
				ID:            "test",
				Type:          "aurora",
				CloudProvider: "aws",
				Inventory: InventoryConfig{
					Source: "wiz",
				},
				EOL: EOLConfig{
					Provider: "", // Missing
					Product:  "test",
				},
			},
		},
	}

	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "eol.provider is required")
}

// TestValidateConfig_Mappings exercises the required_mappings /
// field_mappings split: each resource self-declares what's required,
// every value in required_mappings must be non-empty, and a key may
// appear in at most one of the two maps.
func TestValidateConfig_Mappings(t *testing.T) {
	tests := []struct {
		name       string
		required   map[string]string
		field      map[string]string
		wantErrSub string
	}{
		{
			name:       "missing resource_id fails",
			required:   map[string]string{"version": "v", "engine": "e"},
			wantErrSub: "required_mappings.resource_id is required",
		},
		{
			name:       "empty resource_id fails",
			required:   map[string]string{"resource_id": "", "version": "v", "engine": "e"},
			wantErrSub: "required_mappings.resource_id is required",
		},
		{
			name:       "empty value in required_mappings fails",
			required:   map[string]string{"resource_id": "id", "version": "v", "engine": ""},
			wantErrSub: "required_mappings.engine must not be empty",
		},
		{
			name:       "duplicate key in both maps fails",
			required:   map[string]string{"resource_id": "id", "version": "v"},
			field:      map[string]string{"version": "v2"},
			wantErrSub: `mapping "version" appears in both`,
		},
		{
			name: "minimal valid config (Lambda-style: resource_id only)",
			required: map[string]string{
				"resource_id": "externalId",
			},
		},
		{
			name: "EKS-style: resource_id + version only (engine implicit)",
			required: map[string]string{
				"resource_id": "providerUniqueId",
				"version":     "versionDetails.version",
			},
		},
		{
			name: "Aurora-style: resource_id + version + engine",
			required: map[string]string{
				"resource_id": "externalId",
				"version":     "versionDetails.version",
				"engine":      "typeFields.kind",
			},
			field: map[string]string{
				"name":       "name",
				"account_id": "cloudAccount.externalId",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ResourcesConfig{
				Version: "v1",
				Resources: []ResourceConfig{
					{
						ID:            "test",
						Type:          "aurora",
						CloudProvider: "aws",
						Inventory: InventoryConfig{
							Source:           "wiz",
							RequiredMappings: tt.required,
							FieldMappings:    tt.field,
						},
						EOL: EOLConfig{
							Provider: "endoflife-date",
							Product:  "test",
						},
					},
				},
			}

			err := validateConfig(cfg)
			if tt.wantErrSub == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrSub)
			}
		})
	}
}

// TestValidateConfig_Transforms exercises the at-most-one-op-per-field
// invariant of the transforms DSL. The loader catches copy-paste
// mistakes that would otherwise resolve to a silent precedence at
// runtime — keeping the YAML readable: one field, one op.
func TestValidateConfig_Transforms(t *testing.T) {
	base := func(transforms TransformsConfig) *ResourcesConfig {
		return &ResourcesConfig{
			Version: "v1",
			Resources: []ResourceConfig{
				{
					ID:            "lambda",
					Type:          "lambda",
					CloudProvider: "aws",
					Inventory: InventoryConfig{
						Source: "wiz",
						RequiredMappings: map[string]string{
							"resource_id": "externalId",
						},
					},
					EOL: EOLConfig{
						Provider: "endoflife-date",
						Product:  "aws-lambda",
					},
					Transforms: transforms,
				},
			},
		}
	}

	t.Run("two version ops at once is rejected", func(t *testing.T) {
		err := validateConfig(base(TransformsConfig{
			Version: &VersionTransform{
				StripPrefixes: []string{"foo"},
				ExtractJSONField: &ExtractJSONFieldOp{
					FromColumn: "x", Field: "y",
				},
			},
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "set at most one of strip_prefixes, extract_json_field")
	})

	t.Run("two engine ops at once is rejected", func(t *testing.T) {
		err := validateConfig(base(TransformsConfig{
			Engine: &EngineTransform{
				Constant: "aws-lambda",
				SubstringLookup: []SubstringLookupRule{
					{Contains: []string{"a"}, Result: "b"},
				},
			},
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "set at most one of constant, default_if_empty, substring_lookup, from_version_major")
	})

	t.Run("extract_json_field requires both from_column and field", func(t *testing.T) {
		err := validateConfig(base(TransformsConfig{
			Version: &VersionTransform{
				ExtractJSONField: &ExtractJSONFieldOp{FromColumn: "x"},
			},
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "extract_json_field.field is required")
	})

	t.Run("substring_lookup rejects empty contains", func(t *testing.T) {
		err := validateConfig(base(TransformsConfig{
			Engine: &EngineTransform{
				SubstringLookup: []SubstringLookupRule{
					{Contains: nil, Result: "x"},
				},
			},
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "contains must not be empty")
	})

	t.Run("from_version_major requires default", func(t *testing.T) {
		err := validateConfig(base(TransformsConfig{
			Engine: &EngineTransform{
				FromVersionMajor: &FromVersionMajorOp{
					Majors: map[string]string{"5": "elasticsearch"},
				},
			},
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from_version_major.default is required")
	})

	t.Run("valid lambda-style config passes", func(t *testing.T) {
		err := validateConfig(base(TransformsConfig{
			Version: &VersionTransform{
				ExtractJSONField: &ExtractJSONFieldOp{
					FromColumn: "graphEntity.properties",
					Field:      "runtime",
				},
			},
			Engine: &EngineTransform{Constant: "aws-lambda"},
		}))
		assert.NoError(t, err)
	})
}

func TestValidateConfig_MissingEOLProduct(t *testing.T) {
	cfg := &ResourcesConfig{
		Version: "v1",
		Resources: []ResourceConfig{
			{
				ID:            "test",
				Type:          "aurora",
				CloudProvider: "aws",
				Inventory: InventoryConfig{
					Source: "wiz",
				},
				EOL: EOLConfig{
					Provider: "endoflife-date",
					Product:  "", // Missing
				},
			},
		},
	}

	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "eol.product is required")
}
