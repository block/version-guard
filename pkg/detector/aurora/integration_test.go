package aurora

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
	// Setup: Create a realistic scenario with multiple Aurora clusters
	// representing different upgrade states

	eolDate := time.Now().AddDate(0, -6, 0)       // 6 months ago
	approachingEOL := time.Now().AddDate(0, 2, 0) // 2 months from now
	futureEOL := time.Now().AddDate(2, 0, 0)      // 2 years from now

	// Mock inventory with 5 Aurora clusters in different states
	mockInventory := &inventorymock.InventorySource{
		Resources: []*types.Resource{
			{
				ID:             "arn:aws:rds:us-east-1:123456:cluster:legacy-mysql-56",
				Name:           "legacy-mysql-56",
				Type:           types.ResourceTypeAurora,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "legacy-payments",
				CloudAccountID: "123456",
				CloudRegion:    "us-east-1",
				Brand:          "brand-a",
				CurrentVersion: "5.6.10a",
				Engine:         "aurora-mysql",
			},
			{
				ID:             "arn:aws:rds:us-east-1:123456:cluster:mysql-57-extended",
				Name:           "mysql-57-extended",
				Type:           types.ResourceTypeAurora,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "billing",
				CloudAccountID: "123456",
				CloudRegion:    "us-east-1",
				Brand:          "brand-a",
				CurrentVersion: "5.7.12",
				Engine:         "aurora-mysql",
			},
			{
				ID:             "arn:aws:rds:us-east-1:123456:cluster:mysql-57-approaching",
				Name:           "mysql-57-approaching",
				Type:           types.ResourceTypeAurora,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "analytics",
				CloudAccountID: "123456",
				CloudRegion:    "us-east-1",
				Brand:          "brand-b",
				CurrentVersion: "5.7.44",
				Engine:         "aurora-mysql",
				Tags: map[string]string{
					"app":   "analytics",
					"env":   "production",
					"brand": "brand-b",
				},
			},
			{
				ID:             "arn:aws:rds:us-west-2:789012:cluster:mysql-80-current",
				Name:           "mysql-80-current",
				Type:           types.ResourceTypeAurora,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "payments",
				CloudAccountID: "789012",
				CloudRegion:    "us-west-2",
				Brand:          "brand-a",
				CurrentVersion: "8.0.35",
				Engine:         "aurora-mysql",
			},
			{
				ID:             "arn:aws:rds:eu-west-1:345678:cluster:postgres-11-deprecated",
				Name:           "postgres-11-deprecated",
				Type:           types.ResourceTypeAurora,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "user-service",
				CloudAccountID: "345678",
				CloudRegion:    "eu-west-1",
				Brand:          "brand-c",
				CurrentVersion: "11.21",
				Engine:         "aurora-postgresql",
			},
		},
	}

	// Mock EOL provider with lifecycle data for each version
	deprecationDate := time.Now().AddDate(0, -3, 0) // 3 months ago
	mockEOL := &mock.EOLProvider{
		Versions: map[string]*types.VersionLifecycle{
			// Aurora MySQL 5.6 - Past EOL
			"aurora-mysql:5.6.10a": {
				Version:      "5.6.10a",
				Engine:       "aurora-mysql",
				IsEOL:        true,
				EOLDate:      &eolDate,
				IsSupported:  false,
				IsDeprecated: true,
				Source:       "mock-eol",
				FetchedAt:    time.Now(),
			},
			// Aurora MySQL 5.7.12 - Extended support
			"aurora-mysql:5.7.12": {
				Version:           "5.7.12",
				Engine:            "aurora-mysql",
				IsExtendedSupport: true,
				IsSupported:       true,
				Source:            "mock-eol",
				FetchedAt:         time.Now(),
			},
			// Aurora MySQL 5.7.44 - Approaching EOL
			"aurora-mysql:5.7.44": {
				Version:     "5.7.44",
				Engine:      "aurora-mysql",
				EOLDate:     &approachingEOL,
				IsSupported: true,
				Source:      "mock-eol",
				FetchedAt:   time.Now(),
			},
			// Aurora MySQL 8.0.35 - Current
			"aurora-mysql:8.0.35": {
				Version:     "8.0.35",
				Engine:      "aurora-mysql",
				EOLDate:     &futureEOL,
				IsSupported: true,
				Source:      "mock-eol",
				FetchedAt:   time.Now(),
			},
			// Aurora PostgreSQL 11 - Deprecated
			"aurora-postgresql:11.21": {
				Version:         "11.21",
				Engine:          "aurora-postgresql",
				IsDeprecated:    true,
				DeprecationDate: &deprecationDate,
				IsSupported:     false,
				Source:          "mock-eol",
				FetchedAt:       time.Now(),
			},
		},
	}

	// Create detector with real policy
	detector := NewDetector(
		mockInventory,
		mockEOL,
		policy.NewDefaultPolicy(),
		nil, // logger
	)

	// Execute: Run the full detection flow
	findings, err := detector.Detect(context.Background())

	// Verify: Check results
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(findings) != 5 {
		t.Fatalf("Expected 5 findings, got %d", len(findings))
	}

	// Verify each finding by resource ID
	findingsByID := make(map[string]*types.Finding)
	for _, f := range findings {
		findingsByID[f.ResourceID] = f
	}

	// Test 1: Legacy MySQL 5.6 should be RED (EOL)
	t.Run("Legacy_MySQL_5.6_EOL", func(t *testing.T) {
		finding := findingsByID["arn:aws:rds:us-east-1:123456:cluster:legacy-mysql-56"]
		if finding == nil {
			t.Fatal("Finding not found")
		}

		if finding.Status != types.StatusRed {
			t.Errorf("Expected RED status, got %s", finding.Status)
		}

		if finding.Service != "legacy-payments" {
			t.Errorf("Expected service 'legacy-payments', got '%s'", finding.Service)
		}

		if finding.Brand != "brand-a" {
			t.Errorf("Expected brand 'brand-a', got '%s'", finding.Brand)
		}

		if finding.CurrentVersion != "5.6.10a" {
			t.Errorf("Expected version '5.6.10a', got '%s'", finding.CurrentVersion)
		}

		if finding.Message == "" {
			t.Error("Expected message to be set")
		}

		if finding.Recommendation == "" {
			t.Error("Expected recommendation to be set")
		}

		// Verify message mentions EOL
		if !contains(finding.Message, "End-of-Life") && !contains(finding.Message, "deprecated") {
			t.Errorf("Expected message to mention EOL or deprecated, got: %s", finding.Message)
		}

		// Verify recommendation mentions upgrade
		if !contains(finding.Recommendation, "Upgrade") && !contains(finding.Recommendation, "upgrade") {
			t.Errorf("Expected recommendation to mention upgrade, got: %s", finding.Recommendation)
		}
	})

	// Test 2: MySQL 5.7.12 should be YELLOW (Extended Support)
	t.Run("MySQL_5.7_Extended_Support", func(t *testing.T) {
		finding := findingsByID["arn:aws:rds:us-east-1:123456:cluster:mysql-57-extended"]
		if finding == nil {
			t.Fatal("Finding not found")
		}

		if finding.Status != types.StatusYellow {
			t.Errorf("Expected YELLOW status, got %s", finding.Status)
		}

		if finding.Service != "billing" {
			t.Errorf("Expected service 'billing', got '%s'", finding.Service)
		}

		// Verify message mentions extended support or cost
		if !contains(finding.Message, "extended support") {
			t.Errorf("Expected message to mention extended support, got: %s", finding.Message)
		}
	})

	// Test 3: MySQL 5.7.44 should be YELLOW (Approaching EOL)
	t.Run("MySQL_5.7_Approaching_EOL", func(t *testing.T) {
		finding := findingsByID["arn:aws:rds:us-east-1:123456:cluster:mysql-57-approaching"]
		if finding == nil {
			t.Fatal("Finding not found")
		}

		if finding.Status != types.StatusYellow {
			t.Errorf("Expected YELLOW status, got %s", finding.Status)
		}

		if finding.Service != "analytics" {
			t.Errorf("Expected service 'analytics', got '%s'", finding.Service)
		}

		if finding.Brand != "brand-b" {
			t.Errorf("Expected brand 'brand-b', got '%s'", finding.Brand)
		}

		// Verify message mentions approaching EOL or days
		if !contains(finding.Message, "will reach End-of-Life") && !contains(finding.Message, "days") {
			t.Errorf("Expected message to mention approaching EOL, got: %s", finding.Message)
		}
	})

	// Test 4: MySQL 8.0.35 should be GREEN (Current)
	t.Run("MySQL_8.0_Current", func(t *testing.T) {
		finding := findingsByID["arn:aws:rds:us-west-2:789012:cluster:mysql-80-current"]
		if finding == nil {
			t.Fatal("Finding not found")
		}

		if finding.Status != types.StatusGreen {
			t.Errorf("Expected GREEN status, got %s", finding.Status)
		}

		if finding.Service != "payments" {
			t.Errorf("Expected service 'payments', got '%s'", finding.Service)
		}

		if finding.CloudRegion != "us-west-2" {
			t.Errorf("Expected region 'us-west-2', got '%s'", finding.CloudRegion)
		}

		// Verify message is positive
		if !contains(finding.Message, "supported") {
			t.Errorf("Expected message to mention supported, got: %s", finding.Message)
		}

		if finding.Recommendation != "No action required" {
			t.Errorf("Expected recommendation 'No action required', got: %s", finding.Recommendation)
		}
	})

	// Test 5: PostgreSQL 11 should be RED (Deprecated)
	t.Run("PostgreSQL_11_Deprecated", func(t *testing.T) {
		finding := findingsByID["arn:aws:rds:eu-west-1:345678:cluster:postgres-11-deprecated"]
		if finding == nil {
			t.Fatal("Finding not found")
		}

		if finding.Status != types.StatusRed {
			t.Errorf("Expected RED status, got %s", finding.Status)
		}

		if finding.Service != "user-service" {
			t.Errorf("Expected service 'user-service', got '%s'", finding.Service)
		}

		if finding.Brand != "brand-c" {
			t.Errorf("Expected brand 'brand-c', got '%s'", finding.Brand)
		}

		if finding.Engine != "aurora-postgresql" {
			t.Errorf("Expected engine 'aurora-postgresql', got '%s'", finding.Engine)
		}

		if finding.CloudRegion != "eu-west-1" {
			t.Errorf("Expected region 'eu-west-1', got '%s'", finding.CloudRegion)
		}
	})
}

// TestFullFlow_SummaryStatistics tests that we can generate summary stats from findings
func TestFullFlow_SummaryStatistics(t *testing.T) {
	eolDate := time.Now().AddDate(0, -6, 0)
	futureEOL := time.Now().AddDate(2, 0, 0)

	mockInventory := &inventorymock.InventorySource{
		Resources: []*types.Resource{
			{
				ID:             "arn:aws:rds:us-east-1:123:cluster:cluster-1",
				Type:           types.ResourceTypeAurora,
				CloudProvider:  types.CloudProviderAWS,
				CurrentVersion: "5.6.10a",
				Engine:         "aurora-mysql",
			},
			{
				ID:             "arn:aws:rds:us-east-1:123:cluster:cluster-2",
				Type:           types.ResourceTypeAurora,
				CloudProvider:  types.CloudProviderAWS,
				CurrentVersion: "5.7.12",
				Engine:         "aurora-mysql",
			},
			{
				ID:             "arn:aws:rds:us-east-1:123:cluster:cluster-3",
				Type:           types.ResourceTypeAurora,
				CloudProvider:  types.CloudProviderAWS,
				CurrentVersion: "8.0.35",
				Engine:         "aurora-mysql",
			},
		},
	}

	mockEOL := &mock.EOLProvider{
		Versions: map[string]*types.VersionLifecycle{
			"aurora-mysql:5.6.10a": {
				Version: "5.6.10a", Engine: "aurora-mysql",
				IsEOL: true, EOLDate: &eolDate, IsSupported: false,
			},
			"aurora-mysql:5.7.12": {
				Version: "5.7.12", Engine: "aurora-mysql",
				IsExtendedSupport: true, IsSupported: true,
			},
			"aurora-mysql:8.0.35": {
				Version: "8.0.35", Engine: "aurora-mysql",
				EOLDate: &futureEOL, IsSupported: true,
			},
		},
	}

	detector := NewDetector(mockInventory, mockEOL, policy.NewDefaultPolicy(), nil)
	findings, err := detector.Detect(context.Background())

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Calculate summary statistics
	var redCount, yellowCount, greenCount int
	for _, f := range findings {
		switch f.Status {
		case types.StatusRed:
			redCount++
		case types.StatusYellow:
			yellowCount++
		case types.StatusGreen:
			greenCount++
		}
	}

	// Verify counts
	if redCount != 1 {
		t.Errorf("Expected 1 RED finding, got %d", redCount)
	}

	if yellowCount != 1 {
		t.Errorf("Expected 1 YELLOW finding, got %d", yellowCount)
	}

	if greenCount != 1 {
		t.Errorf("Expected 1 GREEN finding, got %d", greenCount)
	}

	// Calculate compliance percentage (GREEN / TOTAL)
	totalResources := len(findings)
	compliancePercentage := (float64(greenCount) / float64(totalResources)) * 100

	expectedCompliance := 33.33
	if compliancePercentage < expectedCompliance-1 || compliancePercentage > expectedCompliance+1 {
		t.Errorf("Expected compliance ~%.2f%%, got %.2f%%", expectedCompliance, compliancePercentage)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsHelper(s, substr)
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
