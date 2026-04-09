package wiz

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/block/Version-Guard/pkg/types"
)

func TestElastiCacheInventorySource_ListResources_Success(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, "elasticache-report-id").
		Return(WizAPIFixtures.ElastiCacheReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.ElastiCacheCSVData), nil)

	client := NewClient(mockWizClient, time.Hour)
	source := NewElastiCacheInventorySource(client, "elasticache-report-id")

	resources, err := source.ListResources(ctx, types.ResourceTypeElastiCache)

	require.NoError(t, err)
	require.Len(t, resources, 5, "Should have 5 ElastiCache clusters from CSV")

	// Verify: First resource (Redis 6.x EOL)
	r1 := resources[0]
	assert.Equal(t, "arn:aws:elasticache:us-east-1:123456789012:cluster:legacy-redis-001", r1.ID)
	assert.Equal(t, "legacy-redis-001", r1.Name)
	assert.Equal(t, types.ResourceTypeElastiCache, r1.Type)
	assert.Equal(t, types.CloudProviderAWS, r1.CloudProvider)
	assert.Equal(t, "legacy-payments", r1.Service)
	assert.Equal(t, "123456789012", r1.CloudAccountID)
	assert.Equal(t, "us-east-1", r1.CloudRegion)
	assert.Equal(t, "brand-a", r1.Brand)
	assert.Equal(t, "6.2.6", r1.CurrentVersion)
	assert.Equal(t, "redis", r1.Engine)

	// Verify: Second resource (Redis 7.0)
	r2 := resources[1]
	assert.Equal(t, "billing", r2.Service)
	assert.Equal(t, "7.0.7", r2.CurrentVersion)
	assert.Equal(t, "redis", r2.Engine)

	// Verify: Third resource (Redis 7.1 - brand-b)
	r3 := resources[2]
	assert.Equal(t, "analytics", r3.Service)
	assert.Equal(t, "brand-b", r3.Brand)
	assert.Equal(t, "7.1.0", r3.CurrentVersion)

	// Verify: Fourth resource (Memcached - no version)
	r4 := resources[3]
	assert.Equal(t, "session-store", r4.Service)
	assert.Equal(t, "us-west-2", r4.CloudRegion)
	assert.Equal(t, "", r4.CurrentVersion)
	assert.Equal(t, "memcached", r4.Engine)

	// Verify: Fifth resource (Valkey - no version)
	r5 := resources[4]
	assert.Equal(t, "user-service", r5.Service)
	assert.Equal(t, "brand-c", r5.Brand)
	assert.Equal(t, "", r5.CurrentVersion)
	assert.Equal(t, "valkey", r5.Engine)

	// Verify: All resources have discovery timestamp
	for _, r := range resources {
		assert.False(t, r.DiscoveredAt.IsZero(), "Should have discovery timestamp")
	}

	mockWizClient.AssertExpectations(t)
}

func TestElastiCacheInventorySource_ListResources_EmptyReport(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, mock.Anything).
		Return(WizAPIFixtures.ElastiCacheReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.EmptyCSVData), nil)

	client := NewClient(mockWizClient, time.Hour)
	source := NewElastiCacheInventorySource(client, "empty-report-id")

	resources, err := source.ListResources(ctx, types.ResourceTypeElastiCache)

	require.NoError(t, err)
	assert.Empty(t, resources, "Should have no resources from header-only CSV")

	mockWizClient.AssertExpectations(t)
}

func TestElastiCacheInventorySource_ListResources_UnsupportedResourceType(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	client := NewClient(mockWizClient, time.Hour)
	source := NewElastiCacheInventorySource(client, "elasticache-report-id")

	_, err := source.ListResources(ctx, types.ResourceTypeAurora)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported resource type")
}

func TestElastiCacheInventorySource_GetResource_Found(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, mock.Anything).
		Return(WizAPIFixtures.ElastiCacheReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.ElastiCacheCSVData), nil)

	client := NewClient(mockWizClient, time.Hour)
	source := NewElastiCacheInventorySource(client, "elasticache-report-id")

	resource, err := source.GetResource(ctx, types.ResourceTypeElastiCache,
		"arn:aws:elasticache:us-east-1:123456789012:cluster:legacy-redis-001")

	require.NoError(t, err)
	require.NotNil(t, resource)
	assert.Equal(t, "legacy-redis-001", resource.Name)
	assert.Equal(t, "6.2.6", resource.CurrentVersion)

	mockWizClient.AssertExpectations(t)
}

func TestElastiCacheInventorySource_GetResource_NotFound(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, mock.Anything).
		Return(WizAPIFixtures.ElastiCacheReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.ElastiCacheCSVData), nil)

	client := NewClient(mockWizClient, time.Hour)
	source := NewElastiCacheInventorySource(client, "elasticache-report-id")

	_, err := source.GetResource(ctx, types.ResourceTypeElastiCache,
		"arn:aws:elasticache:us-west-2:999999999999:cluster:non-existent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "resource not found")

	mockWizClient.AssertExpectations(t)
}

func TestElastiCacheInventorySource_Name(t *testing.T) {
	mockWizClient := new(MockWizClient)
	client := NewClient(mockWizClient, time.Hour)
	source := NewElastiCacheInventorySource(client, "elasticache-report-id")

	assert.Equal(t, "wiz-elasticache", source.Name())
}

func TestElastiCacheInventorySource_CloudProvider(t *testing.T) {
	mockWizClient := new(MockWizClient)
	client := NewClient(mockWizClient, time.Hour)
	source := NewElastiCacheInventorySource(client, "elasticache-report-id")

	assert.Equal(t, types.CloudProviderAWS, source.CloudProvider())
}

func TestElastiCacheInventorySource_FiltersNonClusterTypes(t *testing.T) {
	ctx := context.Background()

	// CSV with mixed nativeTypes — only cluster rows should be included
	csvData := `externalId,name,nativeType,cloudAccount.externalId,versionDetails.version,region,tags,typeFields.kind
arn:aws:elasticache:us-west-2:123456789012:cluster:my-redis-001,my-redis-001,elastiCache/Redis/cluster,123456789012,7.1.0,us-west-2,"[{""key"":""app"",""value"":""myapp""}]",Redis
arn:aws:elasticache:us-west-2:123456789012:snapshot:my-snapshot,my-snapshot,elasticache#snapshot,123456789012,,us-west-2,[],
arn:aws:elasticache:us-west-2:123456789012:user:default,default,elasticache#user,123456789012,,us-west-2,[],`

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, "elasticache-report-id").
		Return(WizAPIFixtures.ElastiCacheReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(csvData), nil)

	client := NewClient(mockWizClient, time.Hour)
	source := NewElastiCacheInventorySource(client, "elasticache-report-id")

	resources, err := source.ListResources(ctx, types.ResourceTypeElastiCache)

	require.NoError(t, err)
	require.Len(t, resources, 1, "Should only include cluster type, not snapshots or users")
	assert.Equal(t, "my-redis-001", resources[0].Name)

	mockWizClient.AssertExpectations(t)
}

func TestElastiCacheInventorySource_RegistryFallback_WhenTagsMissing(t *testing.T) {
	ctx := context.Background()

	csvData := `externalId,name,nativeType,cloudAccount.externalId,versionDetails.version,region,tags,typeFields.kind
arn:aws:elasticache:us-west-2:999888777666:cluster:untagged-redis-001,untagged-redis-001,elastiCache/Redis/cluster,999888777666,7.1.0,us-west-2,[],Redis`

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, "elasticache-report-id").
		Return(WizAPIFixtures.ElastiCacheReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(csvData), nil)

	mockRegistry := &MockRegistryClient{
		AccountToService: map[string]string{
			"999888777666:us-west-2": "checkout-service",
		},
	}

	client := NewClient(mockWizClient, time.Hour)
	source := NewElastiCacheInventorySource(client, "elasticache-report-id").WithRegistryClient(mockRegistry)

	resources, err := source.ListResources(ctx, types.ResourceTypeElastiCache)

	require.NoError(t, err)
	require.Len(t, resources, 1)

	r := resources[0]
	assert.Equal(t, "checkout-service", r.Service, "Should get service from registry when tags are missing")
	assert.Equal(t, "999888777666", r.CloudAccountID)

	mockWizClient.AssertExpectations(t)
}

func TestNormalizeElastiCacheKind(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Redis", "redis"},
		{"Memcached", "memcached"},
		{"Valkey", "valkey"},
		{"redis", "redis"},
		{"REDIS", "redis"},
		{"", ""},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.expected, normalizeElastiCacheKind(tc.input), "normalizeElastiCacheKind(%q)", tc.input)
	}
}

func TestIsElastiCacheResource(t *testing.T) {
	tests := []struct {
		nativeType string
		expected   bool
	}{
		{"elastiCache/Redis/cluster", true},
		{"elastiCache/Redis/instance", true},
		{"elastiCache/Memcached/cluster", true},
		{"elastiCache/Memcached/instance", true},
		{"elastiCache/Valkey/cluster", true},
		{"elastiCache/Valkey/instance", true},
		{"elasticache#snapshot", false},
		{"elasticache#user", false},
		{"elasticache#usergroup", false},
		{"elasticache#serverlesscache", false},
		{"rds/AmazonAuroraMySQL/cluster", false},
		{"", false},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.expected, isElastiCacheResource(tc.nativeType), "isElastiCacheResource(%q)", tc.nativeType)
	}
}
