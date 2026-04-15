package eks

import (
	"context"
	"testing"
	"time"

	"github.com/block/Version-Guard/pkg/eol/mock"
	inventorymock "github.com/block/Version-Guard/pkg/inventory/mock"
	"github.com/block/Version-Guard/pkg/policy"
	"github.com/block/Version-Guard/pkg/types"
)

// TestFullFlow_MultipleResourcesWithDifferentStatuses tests the complete end-to-end flow
// from inventory fetch → EOL lookup → policy classification → finding generation
//
//nolint:gocognit // integration test complexity is acceptable
func TestFullFlow_MultipleResourcesWithDifferentStatuses(t *testing.T) {
	// Setup: Create a realistic scenario with multiple EKS clusters
	// representing different upgrade states

	eolDate := time.Now().AddDate(0, -6, 0)       // 6 months ago
	approachingEOL := time.Now().AddDate(0, 2, 0) // 2 months from now
	futureEOL := time.Now().AddDate(2, 0, 0)      // 2 years from now

	// Mock inventory with 5 EKS clusters in different states
	mockInventory := &inventorymock.InventorySource{
		Resources: []*types.Resource{
			{
				ID:             "arn:aws:eks:us-east-1:123456:cluster/legacy-k8s-1-23",
				Name:           "legacy-k8s-1-23",
				Type:           types.ResourceTypeEKS,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "legacy-app",
				CloudAccountID: "123456",
				CloudRegion:    "us-east-1",
				Brand:          "brand-a",
				CurrentVersion: "k8s-1.23",
				Engine:         "kubernetes",
			},
			{
				ID:             "arn:aws:eks:us-east-1:123456:cluster/k8s-1-26-extended",
				Name:           "k8s-1-26-extended",
				Type:           types.ResourceTypeEKS,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "billing",
				CloudAccountID: "123456",
				CloudRegion:    "us-east-1",
				Brand:          "brand-a",
				CurrentVersion: "k8s-1.26",
				Engine:         "kubernetes",
			},
			{
				ID:             "arn:aws:eks:us-east-1:123456:cluster/k8s-1-27-approaching",
				Name:           "k8s-1-27-approaching",
				Type:           types.ResourceTypeEKS,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "analytics",
				CloudAccountID: "123456",
				CloudRegion:    "us-east-1",
				Brand:          "brand-b",
				CurrentVersion: "k8s-1.27",
				Engine:         "kubernetes",
				Tags: map[string]string{
					"app":   "analytics",
					"env":   "production",
					"brand": "brand-b",
				},
			},
			{
				ID:             "arn:aws:eks:us-west-2:789012:cluster/k8s-1-29-current",
				Name:           "k8s-1-29-current",
				Type:           types.ResourceTypeEKS,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "payments",
				CloudAccountID: "789012",
				CloudRegion:    "us-west-2",
				Brand:          "brand-a",
				CurrentVersion: "k8s-1.29",
				Engine:         "kubernetes",
			},
			{
				ID:             "arn:aws:eks:eu-west-1:345678:cluster/k8s-1-24-deprecated",
				Name:           "k8s-1-24-deprecated",
				Type:           types.ResourceTypeEKS,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "user-service",
				CloudAccountID: "345678",
				CloudRegion:    "eu-west-1",
				Brand:          "brand-c",
				CurrentVersion: "k8s-1.24",
				Engine:         "kubernetes",
			},
		},
	}

	// Mock EOL provider with lifecycle data for each version
	deprecationDate := time.Now().AddDate(0, -3, 0) // 3 months ago
	mockEOL := &mock.EOLProvider{
		Versions: map[string]*types.VersionLifecycle{
			// 1.23: EOL version (deprecated 6 months ago, no longer supported)
			"kubernetes:k8s-1.23": {
				Version:         "k8s-1.23",
				Engine:          "kubernetes",
				IsEOL:           true,
				EOLDate:         &eolDate,
				IsSupported:     false,
				IsDeprecated:    true,
				DeprecationDate: &eolDate,
			},
			// 1.24: Deprecated version (deprecated 3 months ago, approaching EOL)
			"kubernetes:k8s-1.24": {
				Version:         "k8s-1.24",
				Engine:          "kubernetes",
				IsDeprecated:    true,
				DeprecationDate: &deprecationDate,
				IsSupported:     false,
				EOLDate:         &approachingEOL,
			},
			// 1.26: Extended support (still supported, in extended support phase)
			"kubernetes:k8s-1.26": {
				Version:           "k8s-1.26",
				Engine:            "kubernetes",
				IsExtendedSupport: true,
				IsSupported:       true,
			},
			// 1.27: Approaching EOL (still supported, EOL in 2 months)
			"kubernetes:k8s-1.27": {
				Version:     "k8s-1.27",
				Engine:      "kubernetes",
				IsSupported: true,
				EOLDate:     &approachingEOL,
			},
			// 1.29: Current version (fully supported, EOL far in future)
			"kubernetes:k8s-1.29": {
				Version:     "k8s-1.29",
				Engine:      "kubernetes",
				IsSupported: true,
				EOLDate:     &futureEOL,
			},
		},
	}

	// Create detector with default policy
	detector := NewDetector(
		mockInventory,
		mockEOL,
		policy.NewDefaultPolicy(),
		nil, // logger
	)

	// Execute: Run detection
	findings, err := detector.Detect(context.Background())

	// Assert: No errors
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Assert: Should have findings for all 5 clusters
	if len(findings) != 5 {
		t.Fatalf("Expected 5 findings, got %d", len(findings))
	}

	// Create a map for easier lookup
	findingsByID := make(map[string]*types.Finding)
	for _, f := range findings {
		findingsByID[f.ResourceID] = f
	}

	// Verify each finding

	// 1. EOL version (1.23) → RED
	t.Run("EOL version should be RED", func(t *testing.T) {
		finding := findingsByID["arn:aws:eks:us-east-1:123456:cluster/legacy-k8s-1-23"]
		if finding == nil {
			t.Fatal("Finding not found for legacy-k8s-1-23")
		}

		if finding.Status != types.StatusRed {
			t.Errorf("Expected RED status, got %s", finding.Status)
		}

		if finding.CurrentVersion != "k8s-1.23" {
			t.Errorf("Expected version k8s-1.23, got %s", finding.CurrentVersion)
		}

		if finding.Service != "legacy-app" {
			t.Errorf("Expected service 'legacy-app', got %s", finding.Service)
		}

		if finding.Message == "" {
			t.Error("Expected message to be set")
		}

		if finding.Recommendation == "" {
			t.Error("Expected recommendation to be set")
		}

		if finding.EOLDate == nil {
			t.Error("Expected EOL date to be set")
		}
	})

	// 2. Deprecated version (1.24) → RED
	t.Run("Deprecated version should be RED", func(t *testing.T) {
		finding := findingsByID["arn:aws:eks:eu-west-1:345678:cluster/k8s-1-24-deprecated"]
		if finding == nil {
			t.Fatal("Finding not found for k8s-1-24-deprecated")
		}

		if finding.Status != types.StatusRed {
			t.Errorf("Expected RED status, got %s", finding.Status)
		}

		if finding.Brand != "brand-c" {
			t.Errorf("Expected brand 'brand-c', got %s", finding.Brand)
		}
	})

	// 3. Extended support (1.26) → YELLOW
	t.Run("Extended support version should be YELLOW", func(t *testing.T) {
		finding := findingsByID["arn:aws:eks:us-east-1:123456:cluster/k8s-1-26-extended"]
		if finding == nil {
			t.Fatal("Finding not found for k8s-1-26-extended")
		}

		if finding.Status != types.StatusYellow {
			t.Errorf("Expected YELLOW status, got %s", finding.Status)
		}

		if finding.Service != "billing" {
			t.Errorf("Expected service 'billing', got %s", finding.Service)
		}
	})

	// 4. Approaching EOL (1.27) → YELLOW
	t.Run("Approaching EOL version should be YELLOW", func(t *testing.T) {
		finding := findingsByID["arn:aws:eks:us-east-1:123456:cluster/k8s-1-27-approaching"]
		if finding == nil {
			t.Fatal("Finding not found for k8s-1-27-approaching")
		}

		if finding.Status != types.StatusYellow {
			t.Errorf("Expected YELLOW status, got %s", finding.Status)
		}

		if finding.Brand != "brand-b" {
			t.Errorf("Expected brand 'brand-b', got %s", finding.Brand)
		}
	})

	// 5. Current version (1.29) → GREEN
	t.Run("Current version should be GREEN", func(t *testing.T) {
		finding := findingsByID["arn:aws:eks:us-west-2:789012:cluster/k8s-1-29-current"]
		if finding == nil {
			t.Fatal("Finding not found for k8s-1-29-current")
		}

		if finding.Status != types.StatusGreen {
			t.Errorf("Expected GREEN status, got %s", finding.Status)
		}

		if finding.Service != "payments" {
			t.Errorf("Expected service 'payments', got %s", finding.Service)
		}

		if finding.CloudRegion != "us-west-2" {
			t.Errorf("Expected region 'us-west-2', got %s", finding.CloudRegion)
		}
	})

	// Verify timestamp fields are set
	for _, finding := range findings {
		if finding.DetectedAt.IsZero() {
			t.Errorf("DetectedAt should be set for finding %s", finding.ResourceID)
		}

		if finding.UpdatedAt.IsZero() {
			t.Errorf("UpdatedAt should be set for finding %s", finding.ResourceID)
		}
	}

	// Verify resource metadata is preserved
	t.Run("Resource metadata preservation", func(t *testing.T) {
		for _, finding := range findings {
			if finding.ResourceID == "" {
				t.Error("ResourceID should not be empty")
			}

			if finding.ResourceName == "" {
				t.Error("ResourceName should not be empty")
			}

			if finding.ResourceType != types.ResourceTypeEKS {
				t.Errorf("Expected ResourceType EKS, got %s", finding.ResourceType)
			}

			if finding.CloudProvider != types.CloudProviderAWS {
				t.Errorf("Expected CloudProvider AWS, got %s", finding.CloudProvider)
			}

			if finding.Engine != "kubernetes" {
				t.Errorf("Expected Engine 'kubernetes', got %s", finding.Engine)
			}
		}
	})
}
