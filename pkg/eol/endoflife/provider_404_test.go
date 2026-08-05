package endoflife

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	pkgerrors "github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/Version-Guard/pkg/types"
)

// TestProvider_GetVersionLifecycle_Product404 tests graceful degradation when
// a product doesn't exist on endoflife.date yet (e.g., aurora-mysql pending PR).
// The provider must treat ErrProductNotFound as a recoverable signal and
// return an UNKNOWN lifecycle, not error out.
func TestProvider_GetVersionLifecycle_Product404(t *testing.T) {
	fetchedAt := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	mockClient := &MockClient{
		GetProductCyclesFunc: func(_ context.Context, product string) (ProductCyclesResult, error) {
			return ProductCyclesResult{DataSource: types.LifecycleDataSourceLocalOverride, FetchedAt: fetchedAt}, pkgerrors.Wrapf(ErrProductNotFound, "product %q", product)
		},
	}

	provider, _ := NewProvider(mockClient, "amazon-aurora-mysql", "", 0, nil)

	lifecycle, err := provider.GetVersionLifecycle(context.Background(), "aurora-mysql", "8.0.35")

	// Should NOT return error - graceful degradation
	require.NoError(t, err, "should gracefully handle ErrProductNotFound with UNKNOWN lifecycle")

	// Should return UNKNOWN lifecycle
	assert.NotNil(t, lifecycle, "lifecycle should not be nil")
	assert.Equal(t, "", lifecycle.Version, "Version should be empty for UNKNOWN")
	assert.Equal(t, "aurora-mysql", lifecycle.Engine)
	assert.False(t, lifecycle.IsSupported, "IsSupported should be false for UNKNOWN")
	assert.Equal(t, provider.Name(), lifecycle.Source)
	assert.Equal(t, types.LifecycleUnknownCauseProductNotFound, lifecycle.UnknownCause)
	assert.Equal(t, types.LifecycleDataSourceLocalOverride, lifecycle.DataSource)
	assert.Equal(t, fetchedAt, lifecycle.FetchedAt)
}

// TestProvider_ListAllVersions_Product404 tests that ListAllVersions returns
// empty list (not error) for ErrProductNotFound.
func TestProvider_ListAllVersions_Product404(t *testing.T) {
	mockClient := &MockClient{
		GetProductCyclesFunc: func(_ context.Context, product string) (ProductCyclesResult, error) {
			return ProductCyclesResult{}, pkgerrors.Wrapf(ErrProductNotFound, "product %q", product)
		},
	}

	provider, _ := NewProvider(mockClient, "amazon-aurora-mysql", "", 0, nil)

	versions, err := provider.ListAllVersions(context.Background(), "aurora-mysql")

	require.NoError(t, err, "should return empty list for ErrProductNotFound, not error")
	assert.Empty(t, versions, "should return empty list when product not found")
}

// TestProvider_ListAllVersions_404IsCached pins the cache contract for the
// 404 path: a second serial call for the same product MUST be served from
// the cache rather than re-hitting the upstream API. Without this, every
// lookup against an unknown product (typo / new product / pending PR) would
// pay full HTTP latency on each scan.
func TestProvider_ListAllVersions_404IsCached(t *testing.T) {
	var calls atomic.Int32
	mockClient := &MockClient{
		GetProductCyclesFunc: func(_ context.Context, product string) (ProductCyclesResult, error) {
			calls.Add(1)
			return ProductCyclesResult{}, pkgerrors.Wrapf(ErrProductNotFound, "product %q", product)
		},
	}

	provider, _ := NewProvider(mockClient, "amazon-aurora-mysql", "", 0, nil)

	for i := 0; i < 5; i++ {
		_, err := provider.ListAllVersions(context.Background(), "aurora-mysql")
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), calls.Load(),
		"404 result should be cached — singleflight only collapses concurrent calls, "+
			"so without explicit caching every serial lookup re-hits the API")
}

// TestProvider_GetVersionLifecycle_NonProductErrors tests that real errors
// (not 404) still propagate. These do NOT wrap ErrProductNotFound.
func TestProvider_GetVersionLifecycle_NonProductErrors(t *testing.T) {
	tests := []struct {
		name     string
		errorMsg string
	}{
		{name: "500 server error", errorMsg: "unexpected status code 500: Internal Server Error"},
		{name: "timeout error", errorMsg: "context deadline exceeded"},
		{name: "network error", errorMsg: "connection refused"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockClient{
				GetProductCyclesFunc: func(_ context.Context, _ string) (ProductCyclesResult, error) {
					return ProductCyclesResult{}, errors.New(tt.errorMsg)
				},
			}

			provider, _ := NewProvider(mockClient, "amazon-aurora-mysql", "", 0, nil)

			_, err := provider.GetVersionLifecycle(context.Background(), "aurora-mysql", "8.0.35")

			assert.Error(t, err, "non-404 errors should propagate")
			assert.False(t, errors.Is(err, ErrProductNotFound),
				"non-404 errors must not be misclassified as ErrProductNotFound")
		})
	}
}
