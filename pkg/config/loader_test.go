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
      field_mappings:
        engine: "typeFields.kind"
        version: "versionDetails.version"
        region: "region"
        account_id: "cloudAccount.externalId"
        name: "name"
        external_id: "externalId"
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
	assert.Len(t, res.Inventory.FieldMappings, 6)
	assert.Equal(t, "typeFields.kind", res.Inventory.FieldMappings["engine"])
	assert.Equal(t, "versionDetails.version", res.Inventory.FieldMappings["version"])

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
      field_mappings:
        version: "versionDetails.version"
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
      field_mappings:
        version: "versionDetails.version"
    eol:
      provider: endoflife-date
      product: amazon-eks
      schema: eks_adapter
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
	assert.Equal(t, "eks_adapter", cfg.Resources[1].EOL.Schema)
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
