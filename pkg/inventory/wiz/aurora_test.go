package wiz

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/block/Version-Guard/pkg/registry"
	"github.com/block/Version-Guard/pkg/types"
)

// MockRegistryClient is a simple mock for testing registry integration
type MockRegistryClient struct {
	AccountToService map[string]string // key: "accountID:region"
}

func (m *MockRegistryClient) GetServiceByAWSAccount(ctx context.Context, accountID, region string) (*registry.ServiceInfo, error) {
	key := fmt.Sprintf("%s:%s", accountID, region)
	if serviceName, ok := m.AccountToService[key]; ok {
		return &registry.ServiceInfo{
			ServiceName: serviceName,
			Team:        "test-team",
			Environment: "production",
		}, nil
	}
	return nil, registry.ErrNotFound
}

func TestAuroraInventorySource_ListResources_Success(t *testing.T) {
	ctx := context.Background()

	// Setup: Create mock Wiz client with realistic CSV data
	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, "aurora-report-id").
		Return(WizAPIFixtures.AuroraReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.AuroraCSVData), nil)

	client := NewClient(mockWizClient, time.Hour)
	source := NewAuroraInventorySource(client, "aurora-report-id", nil)

	// Execute: List Aurora resources
	resources, err := source.ListResources(ctx, types.ResourceTypeAurora)

	// Verify: No error
	require.NoError(t, err)
	require.Len(t, resources, 5, "Should have 5 Aurora clusters from CSV")

	// Verify: First resource (legacy-mysql-56)
	r1 := resources[0]
	assert.Equal(t, "arn:aws:rds:us-east-1:123456789012:cluster:legacy-mysql-56", r1.ID)
	assert.Equal(t, "legacy-mysql-56", r1.Name)
	assert.Equal(t, types.ResourceTypeAurora, r1.Type)
	assert.Equal(t, types.CloudProviderAWS, r1.CloudProvider)
	assert.Equal(t, "legacy-payments", r1.Service)
	assert.Equal(t, "123456789012", r1.CloudAccountID)
	assert.Equal(t, "us-east-1", r1.CloudRegion)
	assert.Equal(t, "brand-a", r1.Brand)
	assert.Equal(t, "5.6.10a", r1.CurrentVersion)
	assert.Equal(t, "aurora-mysql", r1.Engine)

	// Verify: Second resource (mysql-57-extended)
	r2 := resources[1]
	assert.Equal(t, "billing", r2.Service)
	assert.Equal(t, "5.7.12", r2.CurrentVersion)

	// Verify: Third resource (mysql-57-approaching) - brand-b
	r3 := resources[2]
	assert.Equal(t, "analytics", r3.Service)
	assert.Equal(t, "brand-b", r3.Brand)
	assert.Equal(t, "5.7.44", r3.CurrentVersion)

	// Verify: Fourth resource (mysql-80-current)
	r4 := resources[3]
	assert.Equal(t, "payments", r4.Service)
	assert.Equal(t, "us-west-2", r4.CloudRegion)
	assert.Equal(t, "789012345678", r4.CloudAccountID)
	assert.Equal(t, "8.0.mysql_aurora.3.05.2", r4.CurrentVersion)

	// Verify: Fifth resource (postgres-11-deprecated) - brand-c
	r5 := resources[4]
	assert.Equal(t, "user-service", r5.Service)
	assert.Equal(t, "brand-c", r5.Brand)
	assert.Equal(t, "eu-west-1", r5.CloudRegion)
	assert.Equal(t, "aurora-postgresql", r5.Engine)
	assert.Equal(t, "11.21", r5.CurrentVersion)

	// Verify: All resources have discovery timestamp
	for _, r := range resources {
		assert.False(t, r.DiscoveredAt.IsZero(), "Should have discovery timestamp")
	}

	mockWizClient.AssertExpectations(t)
}

func TestAuroraInventorySource_ListResources_EmptyReport(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, mock.Anything).
		Return(WizAPIFixtures.AuroraReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.EmptyCSVData), nil)

	client := NewClient(mockWizClient, time.Hour)
	source := NewAuroraInventorySource(client, "empty-report-id", nil)

	// Execute: List resources from empty report (only header row)
	resources, err := source.ListResources(ctx, types.ResourceTypeAurora)

	// Verify: Returns empty list (no data rows, only header)
	require.NoError(t, err)
	assert.Empty(t, resources, "Should have no resources from header-only CSV")

	mockWizClient.AssertExpectations(t)
}

func TestAuroraInventorySource_ListResources_UnsupportedResourceType(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	client := NewClient(mockWizClient, time.Hour)
	source := NewAuroraInventorySource(client, "aurora-report-id", nil)

	// Execute: Request unsupported resource type
	_, err := source.ListResources(ctx, types.ResourceTypeElastiCache)

	// Verify: Error for unsupported type
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported resource type")
}

func TestAuroraInventorySource_GetResource_Found(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, mock.Anything).
		Return(WizAPIFixtures.AuroraReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.AuroraCSVData), nil)

	client := NewClient(mockWizClient, time.Hour)
	source := NewAuroraInventorySource(client, "aurora-report-id", nil)

	// Execute: Get specific resource by ARN
	resource, err := source.GetResource(ctx, types.ResourceTypeAurora,
		"arn:aws:rds:us-east-1:123456789012:cluster:legacy-mysql-56")

	// Verify: Found
	require.NoError(t, err)
	require.NotNil(t, resource)
	assert.Equal(t, "legacy-mysql-56", resource.Name)
	assert.Equal(t, "5.6.10a", resource.CurrentVersion)

	mockWizClient.AssertExpectations(t)
}

func TestAuroraInventorySource_GetResource_NotFound(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, mock.Anything).
		Return(WizAPIFixtures.AuroraReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.AuroraCSVData), nil)

	client := NewClient(mockWizClient, time.Hour)
	source := NewAuroraInventorySource(client, "aurora-report-id", nil)

	// Execute: Get non-existent resource
	_, err := source.GetResource(ctx, types.ResourceTypeAurora,
		"arn:aws:rds:us-west-2:999999999999:cluster:non-existent")

	// Verify: Error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resource not found")

	mockWizClient.AssertExpectations(t)
}

func TestAuroraInventorySource_Name(t *testing.T) {
	mockWizClient := new(MockWizClient)
	client := NewClient(mockWizClient, time.Hour)
	source := NewAuroraInventorySource(client, "aurora-report-id", nil)

	assert.Equal(t, "wiz-aurora", source.Name())
}

func TestAuroraInventorySource_CloudProvider(t *testing.T) {
	mockWizClient := new(MockWizClient)
	client := NewClient(mockWizClient, time.Hour)
	source := NewAuroraInventorySource(client, "aurora-report-id", nil)

	assert.Equal(t, types.CloudProviderAWS, source.CloudProvider())
}

func TestParseAuroraRow_ServiceExtraction(t *testing.T) {
	// This tests the tag parsing and service extraction logic
	// Using realistic Wiz CSV data
	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, mock.Anything).
		Return(WizAPIFixtures.AuroraReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.AuroraCSVData), nil)

	client := NewClient(mockWizClient, time.Hour)
	source := NewAuroraInventorySource(client, "aurora-report-id", nil)

	resources, err := source.ListResources(context.Background(), types.ResourceTypeAurora)
	require.NoError(t, err)

	// Verify service extraction from different tag keys
	serviceMap := make(map[string]string)
	for _, r := range resources {
		serviceMap[r.Name] = r.Service
	}

	assert.Equal(t, "legacy-payments", serviceMap["legacy-mysql-56"], "Should extract from app tag")
	assert.Equal(t, "billing", serviceMap["mysql-57-extended"], "Should extract from application tag")
	assert.Equal(t, "analytics", serviceMap["mysql-57-approaching"], "Should extract from app tag")
	assert.Equal(t, "payments", serviceMap["mysql-80-current"], "Should extract from app tag")
	assert.Equal(t, "user-service", serviceMap["postgres-11-deprecated"], "Should extract from app tag")

	mockWizClient.AssertExpectations(t)
}

func TestAuroraInventorySource_RegistryFallback_WhenTagsMissing(t *testing.T) {
	ctx := context.Background()

	// CSV data with NO app tags (simulating untagged resources)
	csvData := `externalId,name,nativeType,cloudAccount.externalId,versionDetails.version,region,tags,typeFields.kind
arn:aws:rds:us-west-2:999888777666:cluster:untagged-cluster,untagged-cluster,rds/AmazonAuroraMySQL/cluster,999888777666,8.0.mysql_aurora.3.05.2,us-west-2,[],AmazonAuroraMySQL`

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, "aurora-report-id").
		Return(WizAPIFixtures.AuroraReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(csvData), nil)

	// Create registry mock with mapping for account 999888777666
	mockRegistry := &MockRegistryClient{
		AccountToService: map[string]string{
			"999888777666:us-west-2": "checkout-service",
		},
	}

	client := NewClient(mockWizClient, time.Hour)
	source := NewAuroraInventorySource(client, "aurora-report-id", nil).WithRegistryClient(mockRegistry)

	// Execute: List resources
	resources, err := source.ListResources(ctx, types.ResourceTypeAurora)

	// Verify: No error
	require.NoError(t, err)
	require.Len(t, resources, 1)

	// Verify: Service name came from registry (not tags)
	r := resources[0]
	assert.Equal(t, "checkout-service", r.Service, "Should get service from registry when tags are missing")
	assert.Equal(t, "999888777666", r.CloudAccountID)
	assert.Equal(t, "us-west-2", r.CloudRegion)

	mockWizClient.AssertExpectations(t)
}

func TestAuroraInventorySource_RegistryFallback_TagsTakePrecedence(t *testing.T) {
	ctx := context.Background()

	// CSV data WITH app tags (tags should take precedence over registry)
	csvData := `externalId,name,nativeType,cloudAccount.externalId,versionDetails.version,region,tags,typeFields.kind
arn:aws:rds:us-east-1:888777666555:cluster:tagged-cluster,tagged-cluster,rds/AmazonAuroraMySQL/cluster,888777666555,8.0.mysql_aurora.3.05.2,us-east-1,"[{""key"":""app"",""value"":""payments""}]",AmazonAuroraMySQL`

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, "aurora-report-id").
		Return(WizAPIFixtures.AuroraReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(csvData), nil)

	// Create registry mock with DIFFERENT service name
	mockRegistry := &MockRegistryClient{
		AccountToService: map[string]string{
			"888777666555:us-east-1": "billing-service",
		},
	}

	client := NewClient(mockWizClient, time.Hour)
	source := NewAuroraInventorySource(client, "aurora-report-id", nil).WithRegistryClient(mockRegistry)

	// Execute: List resources
	resources, err := source.ListResources(ctx, types.ResourceTypeAurora)

	// Verify: No error
	require.NoError(t, err)
	require.Len(t, resources, 1)

	// Verify: Service name came from TAGS (not registry)
	r := resources[0]
	assert.Equal(t, "payments", r.Service, "Tags should take precedence over registry")

	mockWizClient.AssertExpectations(t)
}

func TestAuroraInventorySource_RegistryFallback_NoRegistry(t *testing.T) {
	ctx := context.Background()

	// CSV data with NO app tags
	csvData := `externalId,name,nativeType,cloudAccount.externalId,versionDetails.version,region,tags,typeFields.kind
arn:aws:rds:eu-west-1:777666555444:cluster:my-service-cluster,my-service-cluster,rds/AmazonAuroraPostgreSQL/cluster,777666555444,15.4,eu-west-1,[],AmazonAuroraPostgreSQL`

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, "aurora-report-id").
		Return(WizAPIFixtures.AuroraReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(csvData), nil)

	// NO registry client configured
	client := NewClient(mockWizClient, time.Hour)
	source := NewAuroraInventorySource(client, "aurora-report-id", nil)
	// Note: Not calling WithRegistryClient()

	// Execute: List resources
	resources, err := source.ListResources(ctx, types.ResourceTypeAurora)

	// Verify: No error
	require.NoError(t, err)
	require.Len(t, resources, 1)

	// Verify: Service name extracted from resource name (fallback)
	r := resources[0]
	assert.Equal(t, "my-service", r.Service, "Should extract service from resource name when no tags and no registry")

	mockWizClient.AssertExpectations(t)
}
