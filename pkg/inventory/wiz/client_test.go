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
	assert.Equal(t, "externalId", rows[0][cols["externalId"]])
	assert.Equal(t, "name", rows[0][cols["name"]])
	assert.Equal(t, "typeFields.kind", rows[0][cols["typeFields.kind"]])

	// Verify: First data row (legacy-mysql-56)
	assert.Equal(t, "arn:aws:rds:us-east-1:123456789012:cluster:legacy-mysql-56", cols.col(rows[1], "externalId"))
	assert.Equal(t, "legacy-mysql-56", cols.col(rows[1], "name"))
	assert.Equal(t, "5.6.10a", cols.col(rows[1], "versionDetails.version"))
	assert.Equal(t, "AmazonAuroraMySQL", cols.col(rows[1], "typeFields.kind"))

	// Verify: All mocks were called
	mockWizClient.AssertExpectations(t)
}

func TestClient_GetReportData_CSVCompleteness(t *testing.T) {
	tests := []struct {
		name         string
		expectedRows int
		csv          string
		wantRows     int
		wantErr      string
	}{
		{name: "valid zero result", expectedRows: 0, csv: WizAPIFixtures.EmptyCSVData, wantRows: 1},
		{name: "missing header", expectedRows: 0, csv: "", wantErr: "has no header"},
		{name: "broken header only", expectedRows: 5, csv: WizAPIFixtures.EmptyCSVData, wantErr: "expected 5 data rows, downloaded 0"},
		{name: "truncated data", expectedRows: 2, csv: "id,name\n1,one\n", wantErr: "expected 2 data rows, downloaded 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := *WizAPIFixtures.AuroraReport
			report.ExpectedRows = tt.expectedRows

			mockWizClient := new(MockWizClient)
			mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
			mockWizClient.On("GetReport", mock.Anything, mock.Anything, mock.Anything).Return(&report, nil)
			mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).Return(NewMockReadCloser(tt.csv), nil)

			rows, err := NewClient(mockWizClient, time.Hour).GetReportData(context.Background(), "report-id")
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Nil(t, rows)
			} else {
				require.NoError(t, err)
				require.Len(t, rows, tt.wantRows)
			}

			mockWizClient.AssertExpectations(t)
		})
	}
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

// TestClient_GetReportData_PerReportIDCache pins the contract that calls
// for different reportIDs do NOT evict each other's cache entries. The
// Version-Guard server fans out one detection workflow per resource type,
// each calling GetReportData with a different reportID through the same
// shared *Client; a single-slot cache thrashes to ~0% hit rate during
// parallel scans.
func TestClient_GetReportData_PerReportIDCache(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil)
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, "report-A").
		Return(WizAPIFixtures.AuroraReport, nil).Once()
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, "report-B").
		Return(WizAPIFixtures.AuroraReport, nil).Once()
	// Each download mock is configured Once() — the test fails if either
	// is called twice (the symptom of cache eviction).
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.AuroraCSVData), nil).Once()
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.AuroraCSVData), nil).Once()

	client := NewClient(mockWizClient, time.Hour)

	// Prime both cache slots.
	_, err := client.GetReportData(ctx, "report-A")
	require.NoError(t, err)
	_, err = client.GetReportData(ctx, "report-B")
	require.NoError(t, err)

	// Subsequent calls in alternating order — should be 100% cache hits.
	for i := 0; i < 10; i++ {
		_, err = client.GetReportData(ctx, "report-A")
		require.NoError(t, err)
		_, err = client.GetReportData(ctx, "report-B")
		require.NoError(t, err)
	}

	mockWizClient.AssertExpectations(t)
}

// TestClient_GetReportData_SingleflightCollapsesConcurrent verifies that
// concurrent fetches for the same reportID collapse onto a single HTTP
// fetch via singleflight, while remaining correct under the race detector.
func TestClient_GetReportData_SingleflightCollapsesConcurrent(t *testing.T) {
	ctx := context.Background()

	mockWizClient := new(MockWizClient)
	// Mock body returns a fresh ReadCloser per call. .Once() on the
	// downstream mocks asserts at most one HTTP fetch happens despite
	// many concurrent callers.
	mockWizClient.On("GetAccessToken", mock.Anything).Return(WizAPIFixtures.AccessToken, nil).Once()
	mockWizClient.On("GetReport", mock.Anything, mock.Anything, "concurrent-report").
		Return(WizAPIFixtures.AuroraReport, nil).Once()
	mockWizClient.On("DownloadReport", mock.Anything, mock.Anything).
		Return(NewMockReadCloser(WizAPIFixtures.AuroraCSVData), nil).Once()

	client := NewClient(mockWizClient, time.Hour)

	const goroutines = 16
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			_, err := client.GetReportData(ctx, "concurrent-report")
			errs <- err
		}()
	}
	for i := 0; i < goroutines; i++ {
		require.NoError(t, <-errs)
	}

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
