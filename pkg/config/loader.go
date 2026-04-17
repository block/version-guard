package config

import (
	"os"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

// LoadResourcesConfig loads and parses the resources configuration file
func LoadResourcesConfig(path string) (*ResourcesConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read config file: %s", path)
	}

	var config ResourcesConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, errors.Wrap(err, "failed to parse YAML config")
	}

	// Validate config
	if err := validateConfig(&config); err != nil {
		return nil, errors.Wrap(err, "invalid configuration")
	}

	return &config, nil
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
	}

	return nil
}
