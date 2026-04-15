package wiz

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestClient_GetReportData_Success(t *testing.T) {
	ctx := context.Background()

	// Setup: Create mock Wiz client with realistic API responses
	mockWizClient := new(MockWizClient)

	// Mock GetAccessToken
	mockWizClient.On("GetAccessToken", mock.Anything).
		Return(WizAPIFixtures.AccessToken, nil)

	// Mock GetReport
	mockWizClient.On("GetReport", mock.Anything, WizAPIFixtures.AccessToken, "aurora-report-id-123").
		Return(WizAPIFixtures.AuroraReport, nil)

	// Mock DownloadReport (returns CSV data)
	mockWizClient.On("DownloadReport", mock.Anything, WizAPIFixtures.AuroraReport.DownloadURL).
		Return(NewMockReadCloser(WizAPIFixtures.AuroraCSVData), nil)

	client := NewClient(mockWizClient, time.Hour)

	// Execute: Get report data
	rows, err := client.GetReportData(ctx, "aurora-report-id-123")

	// Verify: No error
	require.NoError(t, err)
	require.NotNil(t, rows)

	// Verify: CSV parsed correctly
	assert.Len(t, rows, 6, "Should have header + 5 data rows")

	// Verify: Header row (using actual column names from fixture)
	cols := buildColumnIndex(rows[0])
	assert.Equal(t, "externalId", rows[0][cols[colHeaderExternalID]])
	assert.Equal(t, "name", rows[0][cols[colHeaderName]])
	assert.Equal(t, "typeFields.kind", rows[0][cols[colHeaderEngineKind]])

	// Verify: First data row (legacy-mysql-56)
	assert.Equal(t, "arn:aws:rds:us-east-1:123456789012:cluster:legacy-mysql-56", cols.col(rows[1], colHeaderExternalID))
	assert.Equal(t, "legacy-mysql-56", cols.col(rows[1], colHeaderName))
	assert.Equal(t, "5.6.10a", cols.col(rows[1], colHeaderVersion))
	assert.Equal(t, "AmazonAuroraMySQL", cols.col(rows[1], colHeaderEngineKind))

	// Verify: All mocks were called
	mockWizClient.AssertExpectations(t)
}

func TestClient_GetReportData_EmptyReport(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, mock.Anything).Return(WizAPIFixtures.AuroraReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).Return(NewMockReadCloser(WizAPIFixtures.EmptyCSVData), nil)

	client := NewClient(mockWizClient, time.Hour)

	// Execute: Get empty report (only has header row)
	rows, err := client.GetReportData(ctx, "empty-report-id")

	// Verify: Returns header row only (this is valid CSV, not an error)
	require.NoError(t, err)
	require.Len(t, rows, 1, "Should have header row only")

	mockWizClient.AssertExpectations(t)
}

func TestClient_GetReportData_GetAccessTokenError(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).
		Return("", fmt.Errorf("authentication failed"))

	client := NewClient(mockWizClient, time.Hour)

	// Execute: Should fail to get access token
	_, err := client.GetReportData(ctx, "report-id")

	// Verify: Error propagated
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get Wiz access token")

	mockWizClient.AssertExpectations(t)
}

func TestClient_GetReportData_GetReportError(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, "bad-report-id").
		Return(nil, fmt.Errorf("report not found"))

	client := NewClient(mockWizClient, time.Hour)

	// Execute: Should fail to get report
	_, err := client.GetReportData(ctx, "bad-report-id")

	// Verify: Error propagated
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get Wiz report")

	mockWizClient.AssertExpectations(t)
}

func TestClient_GetReportData_DownloadError(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, mock.Anything).Return(WizAPIFixtures.AuroraReport, nil)
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(nil, fmt.Errorf("download failed"))

	client := NewClient(mockWizClient, time.Hour)

	// Execute: Should fail to download
	_, err := client.GetReportData(ctx, "report-id")

	// Verify: Error propagated
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to download Wiz report")

	mockWizClient.AssertExpectations(t)
}

func TestClient_GetReportData_Caching(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)

	// Mock should only be called ONCE due to caching
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil).Once()
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, "cached-report-id").
		Return(WizAPIFixtures.AuroraReport, nil).Once()
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.AuroraCSVData), nil).Once()

	client := NewClient(mockWizClient, time.Hour)

	// First call - should hit Wiz API
	rows1, err1 := client.GetReportData(ctx, "cached-report-id")
	require.NoError(t, err1)
	require.Len(t, rows1, 6)

	// Second call - should use cache (mocks not called again)
	rows2, err2 := client.GetReportData(ctx, "cached-report-id")
	require.NoError(t, err2)
	require.Len(t, rows2, 6)

	// Verify: Same data returned
	assert.Equal(t, rows1, rows2)

	// Verify: Mocks called exactly once
	mockWizClient.AssertExpectations(t)
}

func TestClient_GetReportData_CacheExpiry(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)

	// Mock should be called TWICE (cache expires)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil).Times(2)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, "expiring-report-id").
		Return(WizAPIFixtures.AuroraReport, nil).Times(2)
	// Return fresh CSV data each time - use mock.AnythingOfType for dynamic return
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.AuroraCSVData), nil).Once()
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.AuroraCSVData), nil).Once()

	// Very short cache TTL (1 millisecond)
	client := NewClient(mockWizClient, time.Millisecond)

	// First call
	rows1, err1 := client.GetReportData(ctx, "expiring-report-id")
	require.NoError(t, err1)
	require.Len(t, rows1, 6)

	// Wait for cache to expire
	time.Sleep(10 * time.Millisecond)

	// Second call - cache expired, should fetch again
	rows2, err2 := client.GetReportData(ctx, "expiring-report-id")
	require.NoError(t, err2)
	require.Len(t, rows2, 6)

	// Verify: Mocks called twice
	mockWizClient.AssertExpectations(t)
}
