package generic

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/Version-Guard/pkg/config"
	"github.com/block/Version-Guard/pkg/types"
)

// MockInventorySource is a mock implementation of inventory.InventorySource
type MockInventorySource struct {
	ListResourcesFunc func(ctx context.Context, resourceType types.ResourceType) ([]*types.Resource, error)
	GetResourceFunc   func(ctx context.Context, resourceType types.ResourceType, id string) (*types.Resource, error)
	NameFunc          func() string
	CloudProviderFunc func() types.CloudProvider
}

func (m *MockInventorySource) ListResources(ctx context.Context, resourceType types.ResourceType) ([]*types.Resource, error) {
	if m.ListResourcesFunc != nil {
		return m.ListResourcesFunc(ctx, resourceType)
	}
	return nil, nil
}

func (m *MockInventorySource) GetResource(ctx context.Context, resourceType types.ResourceType, id string) (*types.Resource, error) {
	if m.GetResourceFunc != nil {
		return m.GetResourceFunc(ctx, resourceType, id)
	}
	return nil, nil
}

func (m *MockInventorySource) Name() string {
	if m.NameFunc != nil {
		return m.NameFunc()
	}
	return "mock-inventory"
}

func (m *MockInventorySource) CloudProvider() types.CloudProvider {
	if m.CloudProviderFunc != nil {
		return m.CloudProviderFunc()
	}
	return types.CloudProviderAWS
}

// MockEOLProvider is a mock implementation of eol.Provider
type MockEOLProvider struct {
	GetVersionLifecycleFunc func(ctx context.Context, engine, version string) (*types.VersionLifecycle, error)
	ListAllVersionsFunc     func(ctx context.Context, engine string) ([]*types.VersionLifecycle, error)
	NameFunc                func() string
	EnginesFunc             func() []string
}

func (m *MockEOLProvider) GetVersionLifecycle(ctx context.Context, engine, version string) (*types.VersionLifecycle, error) {
	if m.GetVersionLifecycleFunc != nil {
		return m.GetVersionLifecycleFunc(ctx, engine, version)
	}
	return nil, nil
}

func (m *MockEOLProvider) ListAllVersions(ctx context.Context, engine string) ([]*types.VersionLifecycle, error) {
	if m.ListAllVersionsFunc != nil {
		return m.ListAllVersionsFunc(ctx, engine)
	}
	return nil, nil
}

func (m *MockEOLProvider) Name() string {
	if m.NameFunc != nil {
		return m.NameFunc()
	}
	return "mock-eol"
}

func (m *MockEOLProvider) Engines() []string {
	if m.EnginesFunc != nil {
		return m.EnginesFunc()
	}
	return []string{}
}

// MockVersionPolicy is a mock implementation of policy.VersionPolicy
type MockVersionPolicy struct {
	ClassifyFunc          func(resource *types.Resource, lifecycle *types.VersionLifecycle) types.Status
	GetMessageFunc        func(resource *types.Resource, lifecycle *types.VersionLifecycle, status types.Status) string
	GetRecommendationFunc func(resource *types.Resource, lifecycle *types.VersionLifecycle, status types.Status) string
	NameFunc              func() string
}

func (m *MockVersionPolicy) Classify(resource *types.Resource, lifecycle *types.VersionLifecycle) types.Status {
	if m.ClassifyFunc != nil {
		return m.ClassifyFunc(resource, lifecycle)
	}
	return types.StatusGreen
}

func (m *MockVersionPolicy) GetMessage(resource *types.Resource, lifecycle *types.VersionLifecycle, status types.Status) string {
	if m.GetMessageFunc != nil {
		return m.GetMessageFunc(resource, lifecycle, status)
	}
	return "mock message"
}

func (m *MockVersionPolicy) GetRecommendation(resource *types.Resource, lifecycle *types.VersionLifecycle, status types.Status) string {
	if m.GetRecommendationFunc != nil {
		return m.GetRecommendationFunc(resource, lifecycle, status)
	}
	return "mock recommendation"
}

func (m *MockVersionPolicy) Name() string {
	if m.NameFunc != nil {
		return m.NameFunc()
	}
	return "mock-policy"
}

func TestNewDetector(t *testing.T) {
	cfg := config.ResourceConfig{
		ID:            "test-resource",
		Type:          "aurora",
		CloudProvider: "aws",
	}

	inventory := &MockInventorySource{}
	eol := &MockEOLProvider{}
	policy := &MockVersionPolicy{}
	logger := slog.Default()

	detector := NewDetector(&cfg, inventory, eol, policy, logger)

	assert.NotNil(t, detector)
	assert.Equal(t, "test-resource-detector", detector.Name())
	assert.Equal(t, types.ResourceType("aurora"), detector.ResourceType())
}

func TestNewDetector_NilLogger(t *testing.T) {
	cfg := config.ResourceConfig{
		ID:   "test-resource",
		Type: "aurora",
	}

	detector := NewDetector(&cfg, nil, nil, nil, nil)

	assert.NotNil(t, detector)
	assert.NotNil(t, detector.logger) // Should use default logger
}

func TestDetect_NoResources(t *testing.T) {
	cfg := config.ResourceConfig{
		ID:   "test-resource",
		Type: "aurora",
	}

	inventory := &MockInventorySource{
		ListResourcesFunc: func(ctx context.Context, resourceType types.ResourceType) ([]*types.Resource, error) {
			return []*types.Resource{}, nil
		},
	}

	detector := NewDetector(&cfg, inventory, nil, nil, nil)

	findings, err := detector.Detect(context.Background())
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestDetect_SingleResourceGreen(t *testing.T) {
	cfg := config.ResourceConfig{
		ID:   "aurora-postgresql",
		Type: "aurora",
	}

	testResource := &types.Resource{
		ID:             "arn:aws:rds:us-east-1:123456789012:cluster:test-cluster",
		Name:           "test-cluster",
		Type:           types.ResourceTypeAurora,
		CloudProvider:  types.CloudProviderAWS,
		CloudAccountID: "123456789012",
		CloudRegion:    "us-east-1",
		CurrentVersion: "16.1",
		Engine:         "aurora-postgresql",
		Service:        "test-service",
		Brand:          "test-brand",
	}

	futureDate := time.Now().AddDate(2, 0, 0)
	testLifecycle := &types.VersionLifecycle{
		Version:         "16.1",
		Engine:          "aurora-postgresql",
		EOLDate:         &futureDate,
		DeprecationDate: &futureDate,
		IsSupported:     true,
		IsDeprecated:    false,
		IsEOL:           false,
	}

	inventory := &MockInventorySource{
		ListResourcesFunc: func(ctx context.Context, resourceType types.ResourceType) ([]*types.Resource, error) {
			return []*types.Resource{testResource}, nil
		},
	}

	eol := &MockEOLProvider{
		GetVersionLifecycleFunc: func(ctx context.Context, engine, version string) (*types.VersionLifecycle, error) {
			assert.Equal(t, "aurora-postgresql", engine)
			assert.Equal(t, "16.1", version)
			return testLifecycle, nil
		},
	}

	policy := &MockVersionPolicy{
		ClassifyFunc: func(resource *types.Resource, lifecycle *types.VersionLifecycle) types.Status {
			return types.StatusGreen
		},
		GetMessageFunc: func(resource *types.Resource, lifecycle *types.VersionLifecycle, status types.Status) string {
			return "Version is current"
		},
		GetRecommendationFunc: func(resource *types.Resource, lifecycle *types.VersionLifecycle, status types.Status) string {
			return "No action needed"
		},
	}

	detector := NewDetector(&cfg, inventory, eol, policy, nil)

	findings, err := detector.Detect(context.Background())
	require.NoError(t, err)
	require.Len(t, findings, 1)

	finding := findings[0]
	assert.Equal(t, testResource.ID, finding.ResourceID)
	assert.Equal(t, testResource.Name, finding.ResourceName)
	assert.Equal(t, testResource.Type, finding.ResourceType)
	assert.Equal(t, testResource.Service, finding.Service)
	assert.Equal(t, testResource.CloudAccountID, finding.CloudAccountID)
	assert.Equal(t, testResource.CloudRegion, finding.CloudRegion)
	assert.Equal(t, testResource.CloudProvider, finding.CloudProvider)
	assert.Equal(t, testResource.Brand, finding.Brand)
	assert.Equal(t, testResource.CurrentVersion, finding.CurrentVersion)
	assert.Equal(t, testResource.Engine, finding.Engine)
	assert.Equal(t, types.StatusGreen, finding.Status)
	assert.Equal(t, "Version is current", finding.Message)
	assert.Equal(t, "No action needed", finding.Recommendation)
	assert.Equal(t, testLifecycle.EOLDate, finding.EOLDate)
}

func TestDetect_MultipleResources(t *testing.T) {
	cfg := config.ResourceConfig{
		ID:   "aurora-postgresql",
		Type: "aurora",
	}

	resource1 := &types.Resource{
		ID:             "cluster-1",
		CurrentVersion: "16.1",
		Engine:         "aurora-postgresql",
	}

	resource2 := &types.Resource{
		ID:             "cluster-2",
		CurrentVersion: "15.0",
		Engine:         "aurora-postgresql",
	}

	inventory := &MockInventorySource{
		ListResourcesFunc: func(ctx context.Context, resourceType types.ResourceType) ([]*types.Resource, error) {
			return []*types.Resource{resource1, resource2}, nil
		},
	}

	eol := &MockEOLProvider{
		GetVersionLifecycleFunc: func(ctx context.Context, engine, version string) (*types.VersionLifecycle, error) {
			return &types.VersionLifecycle{
				Version:      version,
				Engine:       engine,
				IsSupported:  true,
				IsDeprecated: false,
				IsEOL:        false,
			}, nil
		},
	}

	policy := &MockVersionPolicy{
		ClassifyFunc: func(resource *types.Resource, lifecycle *types.VersionLifecycle) types.Status {
			if resource.CurrentVersion == "16.1" {
				return types.StatusGreen
			}
			return types.StatusYellow
		},
		GetMessageFunc: func(resource *types.Resource, lifecycle *types.VersionLifecycle, status types.Status) string {
			return "mock message"
		},
		GetRecommendationFunc: func(resource *types.Resource, lifecycle *types.VersionLifecycle, status types.Status) string {
			return "mock recommendation"
		},
	}

	detector := NewDetector(&cfg, inventory, eol, policy, nil)

	findings, err := detector.Detect(context.Background())
	require.NoError(t, err)
	require.Len(t, findings, 2)

	assert.Equal(t, "cluster-1", findings[0].ResourceID)
	assert.Equal(t, types.StatusGreen, findings[0].Status)

	assert.Equal(t, "cluster-2", findings[1].ResourceID)
	assert.Equal(t, types.StatusYellow, findings[1].Status)
}

func TestDetect_ContinuesOnError(t *testing.T) {
	cfg := config.ResourceConfig{
		ID:   "aurora-postgresql",
		Type: "aurora",
	}

	resource1 := &types.Resource{
		ID:             "cluster-1",
		CurrentVersion: "16.1",
		Engine:         "aurora-postgresql",
	}

	resource2 := &types.Resource{
		ID:             "cluster-2",
		CurrentVersion: "15.0",
		Engine:         "aurora-postgresql",
	}

	inventory := &MockInventorySource{
		ListResourcesFunc: func(ctx context.Context, resourceType types.ResourceType) ([]*types.Resource, error) {
			return []*types.Resource{resource1, resource2}, nil
		},
	}

	// EOL provider fails for first resource but succeeds for second
	callCount := 0
	eol := &MockEOLProvider{
		GetVersionLifecycleFunc: func(ctx context.Context, engine, version string) (*types.VersionLifecycle, error) {
			callCount++
			if callCount == 1 {
				return nil, assert.AnError // Fail for first resource
			}
			return &types.VersionLifecycle{
				Version:      version,
				Engine:       engine,
				IsSupported:  true,
				IsDeprecated: false,
				IsEOL:        false,
			}, nil
		},
	}

	policy := &MockVersionPolicy{
		ClassifyFunc: func(resource *types.Resource, lifecycle *types.VersionLifecycle) types.Status {
			return types.StatusGreen
		},
		GetMessageFunc: func(resource *types.Resource, lifecycle *types.VersionLifecycle, status types.Status) string {
			return "mock message"
		},
		GetRecommendationFunc: func(resource *types.Resource, lifecycle *types.VersionLifecycle, status types.Status) string {
			return "mock recommendation"
		},
	}

	detector := NewDetector(&cfg, inventory, eol, policy, nil)

	findings, err := detector.Detect(context.Background())
	require.NoError(t, err)

	// Should only have finding for second resource (first failed)
	require.Len(t, findings, 1)
	assert.Equal(t, "cluster-2", findings[0].ResourceID)
}
