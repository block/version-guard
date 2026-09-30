package override

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/Version-Guard/pkg/eol/endoflife"
	"github.com/block/Version-Guard/pkg/types"
)

// Aurora MySQL minor versions carry their own end-of-standard-support dates
// that arrive well before the major cycle's date. Before per-minor cycles
// existed, every 3.x cluster resolved to cycle "3" (EOL 2028-04-30) and a
// fleet on 3.04 (EOL 2026-10-31) reported GREEN. This test loads the
// repository override through the real provider and checks that the bare
// version strings Wiz reports resolve to the minor cycle, and that a minor
// without paid extended support terminates on its own date rather than
// inheriting the major's.
func TestAuroraMySQLOverrideResolvesMinorCycles(t *testing.T) {
	provider := auroraMySQLOverrideProvider(t)

	tests := []struct {
		version         string
		wantCycle       string
		wantEOL         string // terminal EOLDate
		wantStandardEnd string // DeprecationDate; empty when standard end == EOL
	}{
		{version: "3.04.2", wantCycle: "3.04", wantEOL: "2026-10-31"},
		{version: "3.04.6", wantCycle: "3.04", wantEOL: "2026-10-31"},
		// Deprecated non-LTS minors must not fall back to the major's 2028 date.
		{version: "3.06.0", wantCycle: "3.06", wantEOL: "2025-08-31"},
		{version: "3.07.1", wantCycle: "3.07", wantEOL: "2025-08-31"},
		{version: "3.08.2", wantCycle: "3.08", wantEOL: "2026-08-31"},
		{version: "3.10.1", wantCycle: "3.10", wantEOL: "2028-04-30"},
		{version: "3.11.1", wantCycle: "3.11", wantEOL: "2026-11-13"},
		{version: "8.4.7", wantCycle: "8.4.7", wantEOL: "2027-11-30"},
		// 2.11/2.12 stay available under RDS Extended Support.
		{version: "2.11.5", wantCycle: "2.11", wantEOL: "2029-06-30", wantStandardEnd: "2024-10-31"},
		{version: "2.12.5", wantCycle: "2.12", wantEOL: "2029-06-30", wantStandardEnd: "2024-10-31"},
		// Minor without its own cycle falls back to the major.
		{version: "3.99.0", wantCycle: "3", wantEOL: "2029-07-31", wantStandardEnd: "2028-04-30"},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			lifecycle, err := provider.GetVersionLifecycle(context.Background(), "aurora-mysql", tt.version)
			require.NoError(t, err)
			require.Equal(t, tt.wantCycle, lifecycle.Version, "matched cycle")
			require.NotNil(t, lifecycle.EOLDate)
			require.Equal(t, tt.wantEOL, lifecycle.EOLDate.Format("2006-01-02"), "EOLDate")
			if tt.wantStandardEnd == "" {
				require.Nil(t, lifecycle.DeprecationDate, "no separate standard-support end")
				require.Nil(t, lifecycle.ExtendedSupportEnd, "no extended support")
				return
			}
			require.NotNil(t, lifecycle.DeprecationDate)
			require.Equal(t, tt.wantStandardEnd, lifecycle.DeprecationDate.Format("2006-01-02"), "DeprecationDate")
		})
	}
}

func auroraMySQLOverrideProvider(t *testing.T) *endoflife.Provider {
	t.Helper()
	data, err := os.ReadFile("api/amazon-aurora-mysql.json")
	require.NoError(t, err)
	var cycles []*endoflife.ProductCycle
	require.NoError(t, json.Unmarshal(data, &cycles))

	client := &endoflife.MockClient{
		GetProductCyclesFunc: func(_ context.Context, product string) (endoflife.ProductCyclesResult, error) {
			require.Equal(t, "amazon-aurora-mysql", product)
			return endoflife.ProductCyclesResult{
				Cycles:     cycles,
				FetchedAt:  time.Now(),
				DataSource: types.LifecycleDataSourceLocalOverride,
			}, nil
		},
	}
	provider, err := endoflife.NewProvider(client, "amazon-aurora-mysql", "standard", time.Hour, nil)
	require.NoError(t, err)
	return provider
}
