package endoflife

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/block/Version-Guard/pkg/types"
)

func TestProvider_GetVersionLifecycle_PostgreSQL(t *testing.T) {
	// Mock client with test data (using dates relative to 2026-04-08)
	// Testing with PostgreSQL which uses STANDARD endoflife.date schema
	mockClient := &MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*ProductCycle, error) {
			if product != "amazon-rds-postgresql" {
				t.Errorf("Expected product amazon-rds-postgresql, got %s", product)
			}
			return []*ProductCycle{
				{
					// Current version - still in standard support
					Cycle:           "16.2",
					ReleaseDate:     "2024-05-09",
					Support:         "2028-11-09", // Future
					EOL:             "2028-11-09", // Same as support end
					ExtendedSupport: false,
				},
				{
					// Extended support version - past standard, before EOL
					Cycle:           "14.10",
					ReleaseDate:     "2022-11-10",
					Support:         "2024-11-12", // Past
					EOL:             "2027-11-12", // Future (extended support)
					ExtendedSupport: "2027-11-12",
				},
				{
					// EOL version - past all support dates
					Cycle:           "12.18",
					ReleaseDate:     "2020-11-12",
					Support:         "2024-11-14", // Past
					EOL:             "2024-11-14", // Past (before 2026-04-08)
					ExtendedSupport: false,
				},
			}, nil
		},
	}

	provider, _ := NewProvider(mockClient, "amazon-rds-postgresql", "", 1*time.Hour, nil)

	tests := []struct {
		name           string
		engine         string
		version        string
		wantVersion    string
		wantSupported  bool
		wantDeprecated bool
		wantEOL        bool
	}{
		{
			name:           "current version 16.2",
			engine:         "postgres",
			version:        "16.2",
			wantVersion:    "16.2",
			wantSupported:  true,
			wantDeprecated: false,
			wantEOL:        false,
		},
		{
			name:           "postgresql engine variant",
			engine:         "postgresql",
			version:        "16.2",
			wantVersion:    "16.2",
			wantSupported:  true,
			wantDeprecated: false,
			wantEOL:        false,
		},
		{
			name:           "extended support version 14.10",
			engine:         "postgres",
			version:        "14.10",
			wantVersion:    "14.10",
			wantSupported:  true,  // Still in extended support
			wantDeprecated: true,  // Past standard support
			wantEOL:        false, // Not yet EOL
		},
		{
			name:           "eol version 12.18",
			engine:         "postgres",
			version:        "12.18",
			wantVersion:    "12.18",
			wantSupported:  false, // Past all support
			wantDeprecated: true,  // Deprecated
			wantEOL:        true,  // Past EOL date
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle, err := provider.GetVersionLifecycle(context.Background(), tt.engine, tt.version)
			if err != nil {
				t.Fatalf("GetVersionLifecycle() error = %v", err)
			}

			if lifecycle.Version != tt.wantVersion {
				t.Errorf("Version = %s, want %s", lifecycle.Version, tt.wantVersion)
			}
			if lifecycle.IsSupported != tt.wantSupported {
				t.Errorf("IsSupported = %v, want %v", lifecycle.IsSupported, tt.wantSupported)
			}
			if lifecycle.IsDeprecated != tt.wantDeprecated {
				t.Errorf("IsDeprecated = %v, want %v", lifecycle.IsDeprecated, tt.wantDeprecated)
			}
			if lifecycle.IsEOL != tt.wantEOL {
				t.Errorf("IsEOL = %v, want %v", lifecycle.IsEOL, tt.wantEOL)
			}

			// Verify dates are parsed
			if lifecycle.ReleaseDate == nil {
				t.Error("ReleaseDate should not be nil")
			}
			if lifecycle.DeprecationDate == nil {
				t.Error("DeprecationDate (support end) should not be nil")
			}
			if lifecycle.EOLDate == nil {
				t.Error("EOLDate should not be nil")
			}
		})
	}
}

func TestProvider_ListAllVersions(t *testing.T) {
	mockClient := &MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*ProductCycle, error) {
			return []*ProductCycle{
				{
					Cycle:       "16.2",
					ReleaseDate: "2024-05-09",
					Support:     "2028-11-09",
					EOL:         "2028-11-09",
				},
				{
					Cycle:       "15.6",
					ReleaseDate: "2023-05-11",
					Support:     "2027-11-11",
					EOL:         "2027-11-11",
				},
			}, nil
		},
	}

	provider, _ := NewProvider(mockClient, "amazon-rds-postgresql", "", 1*time.Hour, nil)

	versions, err := provider.ListAllVersions(context.Background(), "postgres")
	if err != nil {
		t.Fatalf("ListAllVersions() error = %v", err)
	}

	if len(versions) != 2 {
		t.Errorf("Got %d versions, want 2", len(versions))
	}

	// Verify first version
	if versions[0].Version != "16.2" {
		t.Errorf("First version = %s, want 16.2", versions[0].Version)
	}
	if versions[0].Engine != "postgres" {
		t.Errorf("First version engine = %s, want postgres", versions[0].Engine)
	}
	if versions[0].Source != "endoflife-date-api" {
		t.Errorf("Source = %s, want endoflife-date-api", versions[0].Source)
	}
}

func TestProvider_Caching(t *testing.T) {
	callCount := 0
	mockClient := &MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*ProductCycle, error) {
			callCount++
			return []*ProductCycle{
				{
					Cycle:       "16.2",
					ReleaseDate: "2024-05-09",
					Support:     "2028-11-09",
					EOL:         "2028-11-09",
				},
			}, nil
		},
	}

	provider, _ := NewProvider(mockClient, "amazon-rds-postgresql", "", 1*time.Hour, nil)

	// First call - should hit API
	_, err := provider.ListAllVersions(context.Background(), "postgres")
	if err != nil {
		t.Fatalf("First call error = %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 API call, got %d", callCount)
	}

	// Second call - should use cache
	_, err = provider.ListAllVersions(context.Background(), "postgres")
	if err != nil {
		t.Fatalf("Second call error = %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 API call (cached), got %d", callCount)
	}

	// Third call - should still use cache
	_, err = provider.GetVersionLifecycle(context.Background(), "postgres", "16.2")
	if err != nil {
		t.Fatalf("Third call error = %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 API call (cached), got %d", callCount)
	}
}

func TestProvider_CacheExpiration(t *testing.T) {
	callCount := 0
	mockClient := &MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*ProductCycle, error) {
			callCount++
			return []*ProductCycle{
				{
					Cycle:       "16.2",
					ReleaseDate: "2024-05-09",
					Support:     "2028-11-09",
					EOL:         "2028-11-09",
				},
			}, nil
		},
	}

	// Very short TTL for testing
	provider, _ := NewProvider(mockClient, "amazon-rds-postgresql", "", 50*time.Millisecond, nil)

	// First call
	_, err := provider.ListAllVersions(context.Background(), "postgres")
	if err != nil {
		t.Fatalf("First call error = %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 API call, got %d", callCount)
	}

	// Wait for cache to expire
	time.Sleep(100 * time.Millisecond)

	// Second call after expiration - should hit API again
	_, err = provider.ListAllVersions(context.Background(), "postgres")
	if err != nil {
		t.Fatalf("Second call error = %v", err)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 API calls (cache expired), got %d", callCount)
	}
}

func TestProvider_VersionNotFound(t *testing.T) {
	mockClient := &MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*ProductCycle, error) {
			return []*ProductCycle{
				{
					Cycle:       "16.2",
					ReleaseDate: "2024-05-09",
					Support:     "2028-11-09",
					EOL:         "2028-11-09",
				},
			}, nil
		},
	}

	provider, _ := NewProvider(mockClient, "amazon-rds-postgresql", "", 1*time.Hour, nil)

	lifecycle, err := provider.GetVersionLifecycle(context.Background(), "postgres", "99.99")
	if err != nil {
		t.Fatalf("Expected no error for unknown version, got %v", err)
	}

	// Should return unsupported lifecycle with empty Version (signals missing data, not unsupported version)
	if lifecycle.IsSupported {
		t.Error("Unknown version should not be supported")
	}
	if lifecycle.Version != "" {
		t.Errorf("Version = %s, want empty string (signals data gap)", lifecycle.Version)
	}
	if lifecycle.Engine != "postgres" {
		t.Errorf("Engine = %s, want postgres", lifecycle.Engine)
	}
}

func TestProvider_Name(t *testing.T) {
	provider, _ := NewProvider(&MockClient{}, "amazon-rds-postgresql", "", 1*time.Hour, nil)
	if name := provider.Name(); name != "endoflife-date-api" {
		t.Errorf("Name() = %s, want endoflife-date-api", name)
	}
}

// TestProvider_Engines pins the per-product binding: a Provider instance
// is constructed for one endoflife.date product, so Engines() echoes back
// exactly that product. Callers iterating multiple providers can use this
// to disambiguate without needing a Go-side engine→product table.
func TestProvider_Engines(t *testing.T) {
	provider, _ := NewProvider(&MockClient{}, "amazon-rds-postgresql", "", 1*time.Hour, nil)
	engines := provider.Engines()

	if len(engines) != 1 || engines[0] != "amazon-rds-postgresql" {
		t.Errorf("Engines() = %v, want [amazon-rds-postgresql]", engines)
	}
}

// TestProvider_DeclarativeLifecycle pins the YAML-driven lifecycle
// wiring: when the resource declares a lifecycle block, the provider
// must dispatch cycle conversion through the declarative adapter so
// product-specific endoflife.date field semantics stay out of Go code.
func TestProvider_DeclarativeLifecycle(t *testing.T) {
	mockClient := &MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*ProductCycle, error) {
			if product != "amazon-eks" {
				t.Errorf("Expected product amazon-eks, got %s", product)
			}
			return []*ProductCycle{
				{
					Cycle:           "1.32",
					ReleaseDate:     "2024-11-19",
					EOL:             "2026-12-19",
					ExtendedSupport: "2027-12-19",
				},
			}, nil
		},
	}

	lifecycleConfig := &DeclarativeLifecycleConfig{
		DeprecationDate: LifecycleDateSource{Field: lifecycleFieldEOL},
		ExtendedSupportEnd: LifecycleDateSource{
			Field:            lifecycleFieldExtendedSupport,
			BoolTrueFallback: lifecycleFieldEOL,
		},
		DeprecatedWindow:    lifecycleActionExtendedSupport,
		PastExtendedSupport: lifecycleActionUnsupported,
	}
	provider, err := NewProviderWithLifecycle(
		mockClient,
		"amazon-eks",
		"declarative",
		lifecycleConfig,
		1*time.Hour,
		nil,
	)
	if err != nil {
		t.Fatalf("NewProviderWithLifecycle() error = %v", err)
	}

	engines := []string{"kubernetes", "k8s", "eks"}
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			versions, err := provider.ListAllVersions(context.Background(), engine)
			if err != nil {
				t.Fatalf("Unexpected error for %s: %v", engine, err)
			}
			if len(versions) != 1 {
				t.Fatalf("Expected 1 version, got %d", len(versions))
			}
			v := versions[0]
			if v.Version != "1.32" {
				t.Errorf("Expected version 1.32, got %s", v.Version)
			}
			// Declarative EKS config: cycle.EOL → DeprecationDate and
			// cycle.ExtendedSupport → ExtendedSupportEnd. EOLDate stays nil
			// because EKS clusters never truly EOL.
			if v.EOLDate != nil {
				t.Errorf("EOLDate = %v, want nil (EKS has no true EOL)", v.EOLDate)
			}
			if v.ExtendedSupportEnd == nil {
				t.Error("ExtendedSupportEnd should be set from cycle.ExtendedSupport")
			}
		})
	}
}

func TestNewProvider_DeclarativeLifecycleRequiresConfig(t *testing.T) {
	_, err := NewProviderWithLifecycle(&MockClient{}, "amazon-eks", "declarative", nil, 1*time.Hour, nil)
	if err == nil {
		t.Fatal("expected error for declarative schema without lifecycle config")
	}
}

// TestNewProvider_InvalidSchema asserts construction-time rejection of
// unknown schema names — the same coverage applies whether the bad
// value comes from a YAML typo or a future adapter that's not yet
// registered. The error message includes the product so operators can
// trace which resource entry caused the failure.
func TestNewProvider_InvalidSchema(t *testing.T) {
	_, err := NewProvider(&MockClient{}, "amazon-eks", "no_such_schema", 1*time.Hour, nil)
	if err == nil {
		t.Fatal("expected error for unknown schema, got nil")
	}
	if !strings.Contains(err.Error(), "no_such_schema") {
		t.Errorf("error %q should mention the bad schema name", err)
	}
	if !strings.Contains(err.Error(), "amazon-eks") {
		t.Errorf("error %q should mention the product for traceability", err)
	}
}

// TestNewProvider_DefaultSchema asserts that an empty schema string is
// treated as "standard" so resources that don't declare eol.schema
// continue to work without any YAML edit.
func TestNewProvider_DefaultSchema(t *testing.T) {
	provider, err := NewProvider(&MockClient{}, "amazon-rds-postgresql", "", 1*time.Hour, nil)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if provider.adapter == nil {
		t.Fatal("adapter must be non-nil after defaulting")
	}
	if _, ok := provider.adapter.(*StandardSchemaAdapter); !ok {
		t.Errorf("adapter type = %T, want *StandardSchemaAdapter", provider.adapter)
	}
}

func TestConvertCycle_ExtendedSupport(t *testing.T) {
	provider, _ := NewProvider(&MockClient{}, "amazon-eks", "", 1*time.Hour, nil)

	//nolint:govet // table-test struct field order chosen for readability
	tests := []struct {
		name                    string
		cycle                   *ProductCycle
		wantIsExtendedSupport   bool
		wantExtendedSupportDate bool
	}{
		{
			name: "extended support as boolean true - in extended window",
			cycle: &ProductCycle{
				Cycle:           "1.32",
				ReleaseDate:     "2024-11-19",
				Support:         "2025-12-19", // Past (relative to 2026-04-08)
				EOL:             "2027-05-19", // Future
				ExtendedSupport: true,
			},
			wantIsExtendedSupport:   true, // In extended support window
			wantExtendedSupportDate: true, // Should have end date
		},
		{
			name: "extended support as date string - in extended window",
			cycle: &ProductCycle{
				Cycle:           "1.31",
				ReleaseDate:     "2024-05-29",
				Support:         "2025-06-29", // Past (relative to 2026-04-08)
				EOL:             "2027-11-29", // Future
				ExtendedSupport: "2027-11-29",
			},
			wantIsExtendedSupport:   true, // In extended support window
			wantExtendedSupportDate: true,
		},
		{
			name: "future version - in standard support",
			cycle: &ProductCycle{
				Cycle:           "1.35",
				ReleaseDate:     "2025-11-19",
				Support:         "2027-12-19", // Future
				EOL:             "2029-05-19", // Far future
				ExtendedSupport: true,
			},
			wantIsExtendedSupport:   false, // Not yet in extended support window
			wantExtendedSupportDate: true,  // Should have end date
		},
		{
			name: "no extended support - EOL",
			cycle: &ProductCycle{
				Cycle:           "1.25",
				ReleaseDate:     "2023-02-21",
				Support:         "2024-03-21", // Past
				EOL:             "2025-08-21", // Past (relative to 2026-04-08)
				ExtendedSupport: false,
			},
			wantIsExtendedSupport:   false,
			wantExtendedSupportDate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle, err := provider.convertCycle("kubernetes", "amazon-eks", tt.cycle)
			if err != nil {
				t.Fatalf("convertCycle() error = %v", err)
			}

			if lifecycle.IsExtendedSupport != tt.wantIsExtendedSupport {
				t.Errorf("IsExtendedSupport = %v, want %v", lifecycle.IsExtendedSupport, tt.wantIsExtendedSupport)
			}

			hasExtendedDate := lifecycle.ExtendedSupportEnd != nil
			if hasExtendedDate != tt.wantExtendedSupportDate {
				t.Errorf("Has ExtendedSupportEnd = %v, want %v", hasExtendedDate, tt.wantExtendedSupportDate)
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	//nolint:govet // table-test struct field order chosen for readability
	tests := []struct {
		name    string
		input   string
		wantErr bool
		want    string
	}{
		{
			name:    "valid date",
			input:   "2024-11-19",
			wantErr: false,
			want:    "2024-11-19",
		},
		{
			name:    "boolean true",
			input:   "true",
			wantErr: true,
		},
		{
			name:    "boolean false",
			input:   "false",
			wantErr: true,
		},
		{
			name:    "invalid format",
			input:   "19-11-2024",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parseDate(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				formatted := parsed.Format("2006-01-02")
				if formatted != tt.want {
					t.Errorf("parseDate() = %s, want %s", formatted, tt.want)
				}
			}
		})
	}
}

// TestProvider_InterfaceCompliance verifies that Provider implements eol.Provider interface
func TestProvider_InterfaceCompliance(t *testing.T) {
	var _ interface {
		GetVersionLifecycle(ctx context.Context, engine, version string) (*types.VersionLifecycle, error)
		ListAllVersions(ctx context.Context, engine string) ([]*types.VersionLifecycle, error)
		Name() string
		Engines() []string
	} = (*Provider)(nil)
}

// TestLatestSupportedVersion exercises the heuristic that picks the
// upgrade target the policy layer recommends. Cycles are passed in
// newest-first to mirror endoflife.date's response ordering.
func TestLatestSupportedVersion(t *testing.T) {
	//nolint:govet // field alignment sacrificed for table-test readability
	tests := []struct {
		name string
		in   []*types.VersionLifecycle
		want string
	}{
		{
			name: "picks newest non-extended supported cycle",
			in: []*types.VersionLifecycle{
				{Version: "17", IsSupported: true, IsExtendedSupport: false},
				{Version: "16", IsSupported: true, IsExtendedSupport: false},
				{Version: "14", IsSupported: true, IsExtendedSupport: true},
				{Version: "12", IsSupported: false},
			},
			want: "17",
		},
		{
			name: "skips unsupported cycles even if newer",
			in: []*types.VersionLifecycle{
				{Version: "18-beta", IsSupported: false},
				{Version: "17", IsSupported: true, IsExtendedSupport: false},
			},
			want: "17",
		},
		{
			name: "falls back to extended-support when nothing is in standard support",
			in: []*types.VersionLifecycle{
				{Version: "14", IsSupported: true, IsExtendedSupport: true},
				{Version: "12", IsSupported: false},
			},
			want: "14",
		},
		{
			name: "returns empty when no cycle is supported",
			in: []*types.VersionLifecycle{
				{Version: "12", IsSupported: false},
				{Version: "11", IsSupported: false},
			},
			want: "",
		},
		{
			name: "returns empty for nil/empty slice",
			in:   nil,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := latestSupportedVersion(tt.in)
			if got != tt.want {
				t.Errorf("latestSupportedVersion = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLatestNonExtendedSupportedVersion exercises the strict variant
// used by the YELLOW IsExtendedSupport path. Empty result is a
// meaningful signal — see provider.go's doc comment — so the
// "all-extended" case is the most important table entry here.
func TestLatestNonExtendedSupportedVersion(t *testing.T) {
	//nolint:govet // field alignment sacrificed for table-test readability
	tests := []struct {
		name string
		in   []*types.VersionLifecycle
		want string
	}{
		{
			name: "picks newest non-extended supported cycle",
			in: []*types.VersionLifecycle{
				{Version: "17", IsSupported: true, IsExtendedSupport: false},
				{Version: "16", IsSupported: true, IsExtendedSupport: false},
				{Version: "14", IsSupported: true, IsExtendedSupport: true},
			},
			want: "17",
		},
		{
			name: "skips extended-support cycles even if newer",
			in: []*types.VersionLifecycle{
				{Version: "17", IsSupported: true, IsExtendedSupport: true},
				{Version: "16", IsSupported: true, IsExtendedSupport: false},
			},
			want: "16",
		},
		{
			name: "all supported cycles in extended support → empty (signals fallback)",
			in: []*types.VersionLifecycle{
				{Version: "13", IsSupported: true, IsExtendedSupport: true},
				{Version: "12", IsSupported: true, IsExtendedSupport: true},
				{Version: "11", IsSupported: false},
			},
			want: "",
		},
		{
			name: "no supported cycles at all → empty",
			in: []*types.VersionLifecycle{
				{Version: "12", IsSupported: false},
				{Version: "11", IsSupported: false},
			},
			want: "",
		},
		{
			name: "nil/empty slice → empty",
			in:   nil,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := latestNonExtendedSupportedVersion(tt.in)
			if got != tt.want {
				t.Errorf("latestNonExtendedSupportedVersion = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestProvider_ListAllVersions_PreservesCycleOrder pins the invariant
// that latestSupportedVersion depends on: cycles flow through
// ListAllVersions in the same order the upstream client returned
// them. If a future refactor sorts/dedupes/groups cycles inside
// ListAllVersions, the recommendation heuristic silently breaks; this
// test fails first.
func TestProvider_ListAllVersions_PreservesCycleOrder(t *testing.T) {
	mockClient := &MockClient{
		GetProductCyclesFunc: func(_ context.Context, _ string) ([]*ProductCycle, error) {
			// Deliberately not in semver order — we want to assert
			// ListAllVersions does NOT reorder.
			return []*ProductCycle{
				{Cycle: "17", ReleaseDate: "2025-02-20", Support: "2030-02-28", EOL: "2030-02-28"},
				{Cycle: "16", ReleaseDate: "2024-02-20", Support: "2029-02-28", EOL: "2029-02-28"},
				{Cycle: "9.6", ReleaseDate: "2016-09-29", Support: "2021-11-11", EOL: "2021-11-11"},
				{Cycle: "12", ReleaseDate: "2019-10-03", Support: "2024-11-14", EOL: "2024-11-14"},
			}, nil
		},
	}
	provider, _ := NewProvider(mockClient, "amazon-rds-postgresql", "", 1*time.Hour, nil)

	versions, err := provider.ListAllVersions(context.Background(), "postgres")
	if err != nil {
		t.Fatalf("ListAllVersions error: %v", err)
	}
	want := []string{"17", "16", "9.6", "12"}
	if len(versions) != len(want) {
		t.Fatalf("got %d cycles, want %d", len(versions), len(want))
	}
	for i, w := range want {
		if versions[i].Version != w {
			t.Errorf("cycle[%d] = %q, want %q", i, versions[i].Version, w)
		}
	}
}

// TestProvider_GetVersionLifecycle_ConcurrentSafe verifies that
// concurrent callers on the same product do not race on shared cached
// *VersionLifecycle pointers. Run with `go test -race` to catch any
// regression that re-introduces the cache-mutation race fixed by
// stamping RecommendedVersion at cache-population time.
func TestProvider_GetVersionLifecycle_ConcurrentSafe(t *testing.T) {
	mockClient := &MockClient{
		GetProductCyclesFunc: func(_ context.Context, _ string) ([]*ProductCycle, error) {
			return []*ProductCycle{
				{Cycle: "17", ReleaseDate: "2025-02-20", Support: "2030-02-28", EOL: "2030-02-28"},
				{Cycle: "16.2", ReleaseDate: "2024-05-09", Support: "2028-11-09", EOL: "2028-11-09"},
				{Cycle: "12.18", ReleaseDate: "2020-11-12", Support: "2024-11-14", EOL: "2024-11-14"},
			}, nil
		},
	}
	provider, _ := NewProvider(mockClient, "amazon-rds-postgresql", "", 1*time.Hour, nil)

	// Prime the cache once on the main goroutine so all subsequent
	// callers hit the cached path and exercise the shared-pointer
	// codepath that previously raced.
	if _, err := provider.ListAllVersions(context.Background(), "postgres"); err != nil {
		t.Fatalf("warm-up ListAllVersions error: %v", err)
	}

	const goroutines = 32
	const callsPerGoroutine = 25
	versionsToQuery := []string{"17", "16.2.3", "12.18", "99.0" /* unknown */}

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < callsPerGoroutine; i++ {
				v := versionsToQuery[i%len(versionsToQuery)]
				lifecycle, err := provider.GetVersionLifecycle(context.Background(), "postgres", v)
				if err != nil {
					t.Errorf("GetVersionLifecycle(%q) error: %v", v, err)
					return
				}
				if lifecycle.RecommendedVersion != "17" {
					t.Errorf("RecommendedVersion = %q, want %q", lifecycle.RecommendedVersion, "17")
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestProvider_GetVersionLifecycle_PopulatesRecommendedVersion verifies
// that every code path through GetVersionLifecycle (exact match,
// prefix match, unknown) stamps the product-wide RecommendedVersion
// onto the returned lifecycle so the policy layer can read it
// without re-querying the provider.
func TestProvider_GetVersionLifecycle_PopulatesRecommendedVersion(t *testing.T) {
	mockClient := &MockClient{
		GetProductCyclesFunc: func(_ context.Context, _ string) ([]*ProductCycle, error) {
			return []*ProductCycle{
				// Newest first — the newest non-extended supported
				// cycle (16.2) is what we expect to surface.
				{Cycle: "16.2", ReleaseDate: "2024-05-09", Support: "2028-11-09", EOL: "2028-11-09"},
				{Cycle: "14.10", ReleaseDate: "2022-11-10", Support: "2024-11-12", EOL: "2027-11-12", ExtendedSupport: "2027-11-12"},
				{Cycle: "12.18", ReleaseDate: "2020-11-12", Support: "2024-11-14", EOL: "2024-11-14"},
			}, nil
		},
	}
	provider, _ := NewProvider(mockClient, "amazon-rds-postgresql", "", 1*time.Hour, nil)

	tests := []struct {
		name              string
		version           string
		wantRecommendedV  string
		wantMatchedCycleV string // empty means we expect the unknown lifecycle
	}{
		{
			name:              "exact match still receives RecommendedVersion",
			version:           "16.2",
			wantRecommendedV:  "16.2",
			wantMatchedCycleV: "16.2",
		},
		{
			name:              "prefix match still receives RecommendedVersion",
			version:           "16.2.3",
			wantRecommendedV:  "16.2",
			wantMatchedCycleV: "16.2",
		},
		{
			name:              "unknown version still receives RecommendedVersion",
			version:           "99.0",
			wantRecommendedV:  "16.2",
			wantMatchedCycleV: "", // unknown lifecycle has empty Version
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle, err := provider.GetVersionLifecycle(context.Background(), "postgres", tt.version)
			if err != nil {
				t.Fatalf("GetVersionLifecycle error: %v", err)
			}
			if lifecycle.RecommendedVersion != tt.wantRecommendedV {
				t.Errorf("RecommendedVersion = %q, want %q", lifecycle.RecommendedVersion, tt.wantRecommendedV)
			}
			if lifecycle.Version != tt.wantMatchedCycleV {
				t.Errorf("Version = %q, want %q", lifecycle.Version, tt.wantMatchedCycleV)
			}
		})
	}
}
