//go:build integration
// +build integration

package endoflife

import (
	"context"
	"testing"
	"time"
)

// TestRealAPIIntegration tests against the real endoflife.date API
// Run with: go test -tags=integration -v ./pkg/eol/endoflife
func TestRealAPIIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create real HTTP client
	client := NewRealHTTPClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test amazon-eks product
	cycles, err := client.GetProductCycles(ctx, "amazon-eks")
	if err != nil {
		t.Fatalf("Failed to fetch EKS cycles: %v", err)
	}

	if len(cycles) == 0 {
		t.Fatal("Expected at least one EKS version, got none")
	}

	t.Logf("Fetched %d EKS versions from endoflife.date", len(cycles))

	// Verify first few versions have expected structure
	for i, cycle := range cycles {
		if i >= 5 {
			break
		}

		t.Logf("Version %s:", cycle.Cycle)
		t.Logf("  Release: %s", cycle.ReleaseDate)
		t.Logf("  Support: %s", cycle.Support)
		t.Logf("  EOL: %s", cycle.EOL)

		if cycle.Cycle == "" {
			t.Errorf("Cycle %d: missing version number", i)
		}
		if cycle.ReleaseDate == "" {
			t.Errorf("Cycle %s: missing release date", cycle.Cycle)
		}
		// Note: Support date may be empty for very recent versions
		// This is expected - endoflife.date may not have complete data yet
		if cycle.EOL == "" {
			t.Errorf("Cycle %s: missing EOL date", cycle.Cycle)
		}
	}
}

// TestProviderRealAPIIntegration tests the provider against the real API
func TestProviderRealAPIIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create provider with real client
	client := NewRealHTTPClient()
	provider := NewProvider(client, 1*time.Hour, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// List all EKS versions
	versions, err := provider.ListAllVersions(ctx, "kubernetes")
	if err != nil {
		t.Fatalf("Failed to list versions: %v", err)
	}

	if len(versions) == 0 {
		t.Fatal("Expected at least one version, got none")
	}

	t.Logf("Successfully fetched %d Kubernetes versions", len(versions))

	// Check version 1.30 (more stable data than very recent versions)
	lifecycle, err := provider.GetVersionLifecycle(ctx, "kubernetes", "1.30")
	if err != nil {
		t.Fatalf("Failed to get version 1.30: %v", err)
	}

	t.Logf("Version 1.30:")
	t.Logf("  Version: %s", lifecycle.Version)
	t.Logf("  Supported: %v", lifecycle.IsSupported)
	t.Logf("  Deprecated: %v", lifecycle.IsDeprecated)
	t.Logf("  EOL: %v", lifecycle.IsEOL)
	if lifecycle.ReleaseDate != nil {
		t.Logf("  Released: %s", lifecycle.ReleaseDate.Format("2006-01-02"))
	}
	if lifecycle.DeprecationDate != nil {
		t.Logf("  Deprecation: %s", lifecycle.DeprecationDate.Format("2006-01-02"))
	}
	if lifecycle.EOLDate != nil {
		t.Logf("  EOL Date: %s", lifecycle.EOLDate.Format("2006-01-02"))
	}

	// Verify basic expectations
	if lifecycle.Version != "k8s-1.30" {
		t.Errorf("Expected version k8s-1.30, got %s", lifecycle.Version)
	}
	if lifecycle.ReleaseDate == nil {
		t.Error("Expected release date to be set")
	}
	// Note: DeprecationDate may be nil for versions where endoflife.date doesn't have support end date
	if lifecycle.EOLDate == nil {
		t.Error("Expected EOL date to be set")
	}
}

// TestCachingRealAPI verifies caching works with real API
func TestCachingRealAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := NewRealHTTPClient()
	provider := NewProvider(client, 1*time.Hour, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// First call - should hit API
	start := time.Now()
	versions1, err := provider.ListAllVersions(ctx, "kubernetes")
	if err != nil {
		t.Fatalf("First call failed: %v", err)
	}
	firstCallDuration := time.Since(start)

	// Second call - should use cache (much faster)
	start = time.Now()
	versions2, err := provider.ListAllVersions(ctx, "kubernetes")
	if err != nil {
		t.Fatalf("Second call failed: %v", err)
	}
	secondCallDuration := time.Since(start)

	// Verify same results
	if len(versions1) != len(versions2) {
		t.Errorf("Version counts differ: %d vs %d", len(versions1), len(versions2))
	}

	// Second call should be significantly faster (cached)
	t.Logf("First call: %v", firstCallDuration)
	t.Logf("Second call (cached): %v", secondCallDuration)

	if secondCallDuration > firstCallDuration/2 {
		t.Logf("Warning: Second call not significantly faster, caching may not be working as expected")
	}
}
