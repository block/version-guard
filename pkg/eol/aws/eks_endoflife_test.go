package aws

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"

	"github.com/block/Version-Guard/pkg/eol/endoflife"
)

func TestEnrichWithEndOfLife_Success(t *testing.T) {
	// Mock endoflife.date client using EKS-specific schema:
	// - eol field = end of STANDARD support
	// - extendedSupport field = end of EXTENDED support
	mockClient := &endoflife.MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*endoflife.ProductCycle, error) {
			if product != "amazon-eks" {
				t.Errorf("Expected product amazon-eks, got %s", product)
			}
			return []*endoflife.ProductCycle{
				{
					Cycle:           "1.35",
					ReleaseDate:     "2025-11-20", // Different from static
					EOL:             "2027-12-20", // End of STANDARD support (EKS-specific)
					ExtendedSupport: "2029-05-20", // End of EXTENDED support (true EOL)
				},
			}, nil
		},
	}

	version := &EKSVersion{
		KubernetesVersion: "1.35",
	}

	// Enrich with endoflife.date client
	enrichWithLifecycleDates(context.Background(), version, mockClient)

	// Verify dates came from endoflife.date (not static)
	if version.ReleaseDate == nil {
		t.Fatal("ReleaseDate should not be nil")
	}
	expectedRelease, _ := time.Parse("2006-01-02", "2025-11-20")
	if !version.ReleaseDate.Equal(expectedRelease) {
		t.Errorf("ReleaseDate = %v, want %v (from endoflife.date)", version.ReleaseDate, expectedRelease)
	}

	if version.EndOfStandardDate == nil {
		t.Fatal("EndOfStandardDate should not be nil")
	}
	expectedSupport, _ := time.Parse("2006-01-02", "2027-12-20")
	if !version.EndOfStandardDate.Equal(expectedSupport) {
		t.Errorf("EndOfStandardDate = %v, want %v (from endoflife.date)", version.EndOfStandardDate, expectedSupport)
	}

	if version.EndOfExtendedDate == nil {
		t.Fatal("EndOfExtendedDate should not be nil")
	}
	expectedEOL, _ := time.Parse("2006-01-02", "2029-05-20")
	if !version.EndOfExtendedDate.Equal(expectedEOL) {
		t.Errorf("EndOfExtendedDate = %v, want %v (from endoflife.date)", version.EndOfExtendedDate, expectedEOL)
	}

	// Verify status was updated
	if version.Status != "standard" {
		t.Errorf("Status = %s, want standard", version.Status)
	}
}

func TestEnrichWithEndOfLife_FallbackToStatic(t *testing.T) {
	// Mock client that returns error (simulates API failure)
	mockClient := &endoflife.MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*endoflife.ProductCycle, error) {
			return nil, context.DeadlineExceeded
		},
	}

	version := &EKSVersion{
		KubernetesVersion: "1.31",
	}

	// Enrich with failing endoflife.date client - should fall back to static
	enrichWithLifecycleDates(context.Background(), version, mockClient)

	// Verify dates came from static fallback
	if version.ReleaseDate == nil {
		t.Fatal("ReleaseDate should not be nil (from static fallback)")
	}
	expectedRelease, _ := time.Parse("2006-01-02", "2024-11-19")
	if !version.ReleaseDate.Equal(expectedRelease) {
		t.Errorf("ReleaseDate = %v, want %v (from static)", version.ReleaseDate, expectedRelease)
	}

	if version.EndOfStandardDate == nil {
		t.Fatal("EndOfStandardDate should not be nil (from static fallback)")
	}
	expectedSupport, _ := time.Parse("2006-01-02", "2025-12-19")
	if !version.EndOfStandardDate.Equal(expectedSupport) {
		t.Errorf("EndOfStandardDate = %v, want %v (from static)", version.EndOfStandardDate, expectedSupport)
	}
}

func TestEnrichWithEndOfLife_VersionNotFound(t *testing.T) {
	// Mock client that returns data but not for our version
	mockClient := &endoflife.MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*endoflife.ProductCycle, error) {
			return []*endoflife.ProductCycle{
				{
					Cycle:       "1.30",
					ReleaseDate: "2024-05-29",
					Support:     "2025-06-29",
					EOL:         "2026-11-29",
				},
			}, nil
		},
	}

	version := &EKSVersion{
		KubernetesVersion: "1.31",
	}

	// Enrich - version not in endoflife.date, should fall back to static
	enrichWithLifecycleDates(context.Background(), version, mockClient)

	// Verify dates came from static fallback
	if version.ReleaseDate == nil {
		t.Fatal("ReleaseDate should not be nil (from static fallback)")
	}
	expectedRelease, _ := time.Parse("2006-01-02", "2024-11-19")
	if !version.ReleaseDate.Equal(expectedRelease) {
		t.Errorf("ReleaseDate = %v, want %v (from static)", version.ReleaseDate, expectedRelease)
	}
}

func TestEnrichWithEndOfLife_NoClient(t *testing.T) {
	version := &EKSVersion{
		KubernetesVersion: "1.31",
	}

	// Enrich without endoflife.date client - should use static
	enrichWithLifecycleDates(context.Background(), version, nil)

	// Verify dates came from static
	if version.ReleaseDate == nil {
		t.Fatal("ReleaseDate should not be nil (from static)")
	}
	expectedRelease, _ := time.Parse("2006-01-02", "2024-11-19")
	if !version.ReleaseDate.Equal(expectedRelease) {
		t.Errorf("ReleaseDate = %v, want %v (from static)", version.ReleaseDate, expectedRelease)
	}
}

func TestEKSEOLProvider_WithEndOfLifeClient(t *testing.T) {
	// Mock EKS client
	mockEKSClient := new(MockEKSClient)
	mockEKSClient.On("DescribeAddonVersions", mock.Anything).Return([]*EKSVersion{
		{
			KubernetesVersion: "1.35",
		},
	}, nil)

	// Mock endoflife client with EKS-specific schema
	mockEOLClient := &endoflife.MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*endoflife.ProductCycle, error) {
			return []*endoflife.ProductCycle{
				{
					Cycle:           "1.35",
					ReleaseDate:     "2025-11-20", // Different from static
					EOL:             "2027-12-20", // End of standard support
					ExtendedSupport: "2029-05-20", // End of extended support
				},
			}, nil
		},
	}

	// Create provider with endoflife.date integration
	provider := NewEKSEOLProvider(mockEKSClient, 1*time.Hour).
		WithEndOfLifeClient(mockEOLClient)

	// Fetch versions
	versions, err := provider.ListAllVersions(context.Background(), "kubernetes")
	if err != nil {
		t.Fatalf("ListAllVersions() error = %v", err)
	}

	if len(versions) != 1 {
		t.Fatalf("Expected 1 version, got %d", len(versions))
	}

	// Verify dates came from endoflife.date
	version := versions[0]
	if version.ReleaseDate == nil {
		t.Fatalf("ReleaseDate should not be nil. Version: %+v", version)
	}

	expectedRelease, _ := time.Parse("2006-01-02", "2025-11-20")
	if !version.ReleaseDate.Equal(expectedRelease) {
		t.Errorf("ReleaseDate = %v, want %v (from endoflife.date via provider)", version.ReleaseDate, expectedRelease)
	}
}

func TestEnrichWithEndOfLife_AlreadyHasDates(t *testing.T) {
	// Mock client that should NOT be called
	callCount := 0
	mockClient := &endoflife.MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*endoflife.ProductCycle, error) {
			callCount++
			t.Error("Client should not be called when dates already exist")
			return nil, nil
		},
	}

	// Version already has all dates
	releaseDate, _ := time.Parse("2006-01-02", "2024-01-01")
	supportDate, _ := time.Parse("2006-01-02", "2025-01-01")
	eolDate, _ := time.Parse("2006-01-02", "2026-01-01")

	version := &EKSVersion{
		KubernetesVersion: "1.31",
		ReleaseDate:       &releaseDate,
		EndOfStandardDate: &supportDate,
		EndOfExtendedDate: &eolDate,
	}

	// Enrich - should skip because dates already exist
	enrichWithLifecycleDates(context.Background(), version, mockClient)

	if callCount != 0 {
		t.Error("Client should not have been called when dates already exist")
	}

	// Verify dates unchanged
	if !version.ReleaseDate.Equal(releaseDate) {
		t.Error("ReleaseDate should be unchanged")
	}
}
