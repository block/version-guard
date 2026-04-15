package wiz

import (
	"testing"
)

func TestExtractServiceFromName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "service with environment and resource suffix",
			input:    "my-service-prod-cluster",
			expected: "my-service",
		},
		{
			name:     "service with staging environment",
			input:    "payment-api-staging-db",
			expected: "payment-api",
		},
		{
			name:     "service with development suffix",
			input:    "auth-service-development-database",
			expected: "auth-service",
		},
		{
			name:     "service with resource suffix only",
			input:    "cache-service-redis",
			expected: "cache-service",
		},
		{
			name:     "environment at start (no extraction)",
			input:    "prod-service",
			expected: "prod-service",
		},
		{
			name:     "no suffixes",
			input:    "simple-service",
			expected: "simple-service",
		},
		{
			name:     "single word",
			input:    "database",
			expected: "database",
		},
		{
			name:     "multiple hyphens with qa env",
			input:    "my-complex-service-qa-cluster",
			expected: "my-complex-service",
		},
		{
			name:     "ambiguous - cache is both service and suffix",
			input:    "cache-prod-valkey",
			expected: "cache-prod-valkey", // "cache" is in suffix list, so entire string returned
		},
		{
			name:     "valkey suffix without ambiguity",
			input:    "session-store-prod-valkey",
			expected: "session-store",
		},
		{
			name:     "memcached suffix",
			input:    "session-store-stage-memcached",
			expected: "session-store",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractServiceFromName(tt.input)
			if result != tt.expected {
				t.Errorf("extractServiceFromName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsEnvironmentSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"dev", true},
		{"development", true},
		{"staging", true},
		{"stage", true},
		{"prod", true},
		{"production", true},
		{"test", true},
		{"qa", true},
		{"PROD", true},       // case insensitive
		{"Production", true}, // case insensitive
		{"other", false},
		{"service", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isEnvironmentSuffix(tt.input)
			if result != tt.expected {
				t.Errorf("isEnvironmentSuffix(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsCommonSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"cluster", true},
		{"db", true},
		{"database", true},
		{"rds", true},
		{"aurora", true},
		{"cache", true},
		{"redis", true},
		{"memcached", true},
		{"valkey", true},
		{"CLUSTER", true},  // case insensitive
		{"Database", true}, // case insensitive
		{"service", false},
		{"other", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isCommonSuffix(tt.input)
			if result != tt.expected {
				t.Errorf("isCommonSuffix(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
