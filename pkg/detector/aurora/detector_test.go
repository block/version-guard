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

func TestDetector_Detect_EOLVersion(t *testing.T) {
	// Setup mock inventory with EOL Aurora cluster
	eolDate := time.Now().AddDate(0, -6, 0) // 6 months ago
	mockInventory := &inventorymock.InventorySource{
		Resources: []*types.Resource{
			{
				ID:             "arn:aws:rds:us-east-1:123456:cluster:test-cluster",
				Name:           "test-cluster",
				Type:           types.ResourceTypeAurora,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "test-service",
				CloudAccountID: "123456",
				CloudRegion:    "us-east-1",
				Brand:          "brand-a",
				CurrentVersion: "5.6.10a",
				Engine:         "aurora-mysql",
			},
		},
	}

	// Setup mock EOL provider with EOL version
	mockEOL := &mock.EOLProvider{
		Versions: map[string]*types.VersionLifecycle{
			"aurora-mysql:5.6.10a": {
				Version:     "5.6.10a",
				Engine:      "aurora-mysql",
				IsEOL:       true,
				EOLDate:     &eolDate,
				IsSupported: false,
			},
		},
	}

	// Create detector
	detector := NewDetector(
		mockInventory,
		mockEOL,
		policy.NewDefaultPolicy(),
		nil, // logger
	)

	// Run detection
	findings, err := detector.Detect(context.Background())

	// Assertions
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(findings) != 1 {
		t.Fatalf("Expected 1 finding, got %d", len(findings))
	}

	finding := findings[0]

	if finding.Status != types.StatusRed {
		t.Errorf("Expected RED status for EOL version, got %s", finding.Status)
	}

	if finding.ResourceID != "arn:aws:rds:us-east-1:123456:cluster:test-cluster" {
		t.Errorf("Unexpected resource ID: %s", finding.ResourceID)
	}

	if finding.CurrentVersion != "5.6.10a" {
		t.Errorf("Unexpected version: %s", finding.CurrentVersion)
	}

	if finding.Engine != "aurora-mysql" {
		t.Errorf("Unexpected engine: %s", finding.Engine)
	}
}

func TestDetector_Detect_CurrentVersion(t *testing.T) {
	// Setup mock inventory with current Aurora cluster
	eolDate := time.Now().AddDate(2, 0, 0) // 2 years from now
	mockInventory := &inventorymock.InventorySource{
		Resources: []*types.Resource{
			{
				ID:             "arn:aws:rds:us-west-2:789012:cluster:prod-cluster",
				Name:           "prod-cluster",
				Type:           types.ResourceTypeAurora,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "payments",
				CloudAccountID: "789012",
				CloudRegion:    "us-west-2",
				Brand:          "brand-a",
				CurrentVersion: "8.0.35",
				Engine:         "aurora-mysql",
			},
		},
	}

	// Setup mock EOL provider with supported version
	mockEOL := &mock.EOLProvider{
		Versions: map[string]*types.VersionLifecycle{
			"aurora-mysql:8.0.35": {
				Version:     "8.0.35",
				Engine:      "aurora-mysql",
				EOLDate:     &eolDate,
				IsSupported: true,
			},
		},
	}

	// Create detector
	detector := NewDetector(
		mockInventory,
		mockEOL,
		policy.NewDefaultPolicy(),
		nil, // logger
	)

	// Run detection
	findings, err := detector.Detect(context.Background())

	// Assertions
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(findings) != 1 {
		t.Fatalf("Expected 1 finding, got %d", len(findings))
	}

	finding := findings[0]

	if finding.Status != types.StatusGreen {
		t.Errorf("Expected GREEN status for current version, got %s", finding.Status)
	}
}

func TestDetector_Detect_ExtendedSupport(t *testing.T) {
	// Setup mock inventory
	mockInventory := &inventorymock.InventorySource{
		Resources: []*types.Resource{
			{
				ID:             "arn:aws:rds:us-east-1:123456:cluster:legacy-cluster",
				Name:           "legacy-cluster",
				Type:           types.ResourceTypeAurora,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "legacy-app",
				CloudAccountID: "123456",
				CloudRegion:    "us-east-1",
				Brand:          "brand-a",
				CurrentVersion: "5.7.12",
				Engine:         "aurora-mysql",
			},
		},
	}

	// Setup mock EOL provider with extended support version
	mockEOL := &mock.EOLProvider{
		Versions: map[string]*types.VersionLifecycle{
			"aurora-mysql:5.7.12": {
				Version:           "5.7.12",
				Engine:            "aurora-mysql",
				IsExtendedSupport: true,
				IsSupported:       true,
			},
		},
	}

	// Create detector
	detector := NewDetector(
		mockInventory,
		mockEOL,
		policy.NewDefaultPolicy(),
		nil, // logger
	)

	// Run detection
	findings, err := detector.Detect(context.Background())

	// Assertions
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(findings) != 1 {
		t.Fatalf("Expected 1 finding, got %d", len(findings))
	}

	finding := findings[0]

	if finding.Status != types.StatusYellow {
		t.Errorf("Expected YELLOW status for extended support, got %s", finding.Status)
	}

	if finding.Message == "" {
		t.Error("Expected message to be set")
	}

	if finding.Recommendation == "" {
		t.Error("Expected recommendation to be set")
	}
}

func TestDetector_Detect_MultipleResources(t *testing.T) {
	// Setup mock inventory with multiple clusters
	eolDate := time.Now().AddDate(0, -6, 0)
	futureEOL := time.Now().AddDate(2, 0, 0)

	mockInventory := &inventorymock.InventorySource{
		Resources: []*types.Resource{
			{
				ID:             "arn:aws:rds:us-east-1:123456:cluster:eol-cluster",
				Name:           "eol-cluster",
				Type:           types.ResourceTypeAurora,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "old-app",
				CurrentVersion: "5.6.10a",
				Engine:         "aurora-mysql",
			},
			{
				ID:             "arn:aws:rds:us-east-1:123456:cluster:current-cluster",
				Name:           "current-cluster",
				Type:           types.ResourceTypeAurora,
				CloudProvider:  types.CloudProviderAWS,
				Service:        "new-app",
				CurrentVersion: "8.0.35",
				Engine:         "aurora-mysql",
			},
		},
	}

	// Setup mock EOL provider
	mockEOL := &mock.EOLProvider{
		Versions: map[string]*types.VersionLifecycle{
			"aurora-mysql:5.6.10a": {
				Version:     "5.6.10a",
				Engine:      "aurora-mysql",
				IsEOL:       true,
				EOLDate:     &eolDate,
				IsSupported: false,
			},
			"aurora-mysql:8.0.35": {
				Version:     "8.0.35",
				Engine:      "aurora-mysql",
				EOLDate:     &futureEOL,
				IsSupported: true,
			},
		},
	}

	// Create detector
	detector := NewDetector(
		mockInventory,
		mockEOL,
		policy.NewDefaultPolicy(),
		nil, // logger
	)

	// Run detection
	findings, err := detector.Detect(context.Background())

	// Assertions
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(findings) != 2 {
		t.Fatalf("Expected 2 findings, got %d", len(findings))
	}

	// Verify first finding is RED
	var redFound, greenFound bool
	for _, finding := range findings {
		if finding.Status == types.StatusRed {
			redFound = true
			if finding.ResourceID != "arn:aws:rds:us-east-1:123456:cluster:eol-cluster" {
				t.Errorf("Unexpected RED finding resource: %s", finding.ResourceID)
			}
		}
		if finding.Status == types.StatusGreen {
			greenFound = true
			if finding.ResourceID != "arn:aws:rds:us-east-1:123456:cluster:current-cluster" {
				t.Errorf("Unexpected GREEN finding resource: %s", finding.ResourceID)
			}
		}
	}

	if !redFound {
		t.Error("Expected to find RED status finding")
	}
	if !greenFound {
		t.Error("Expected to find GREEN status finding")
	}
}

func TestDetector_Detect_EmptyInventory(t *testing.T) {
	// Setup empty mock inventory
	mockInventory := &inventorymock.InventorySource{
		Resources: []*types.Resource{},
	}

	mockEOL := &mock.EOLProvider{
		Versions: map[string]*types.VersionLifecycle{},
	}

	// Create detector
	detector := NewDetector(
		mockInventory,
		mockEOL,
		policy.NewDefaultPolicy(),
		nil, // logger
	)

	// Run detection
	findings, err := detector.Detect(context.Background())

	// Assertions
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(findings) != 0 {
		t.Errorf("Expected 0 findings for empty inventory, got %d", len(findings))
	}
}

func TestDetector_Name(t *testing.T) {
	detector := NewDetector(nil, nil, nil, nil)

	name := detector.Name()
	expected := "aurora-detector"

	if name != expected {
		t.Errorf("Expected name '%s', got '%s'", expected, name)
	}
}

func TestDetector_ResourceType(t *testing.T) {
	detector := NewDetector(nil, nil, nil, nil)

	resourceType := detector.ResourceType()

	if resourceType != types.ResourceTypeAurora {
		t.Errorf("Expected resource type AURORA, got %s", resourceType)
	}
}
