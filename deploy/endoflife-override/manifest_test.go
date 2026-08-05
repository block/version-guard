package override

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateManifest(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(t *testing.T, root string)
		wantErr     string
		wantWarning string
	}{
		{name: "valid manifest"},
		{name: "duplicate product", mutate: mutateManifest(func(m *manifest) { m.Overrides = append(m.Overrides, m.Overrides[0]) }), wantErr: "duplicate product"},
		{name: "missing API file entry", mutate: func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, "api", "unlisted.json"), []byte("[]"), 0o600))
		}, wantErr: "has no manifest entry"},
		{name: "entry references missing file", mutate: func(t *testing.T, root string) {
			require.NoError(t, os.Remove(filepath.Join(root, "api", "amazon-aurora-mysql.json")))
		}, wantErr: "does not exist"},
		{name: "invalid source URL", mutate: mutateManifest(func(m *manifest) { m.Overrides[0].SourceURL = "http://example.com/source" }), wantErr: "must use https"},
		{name: "malformed source URL", mutate: mutateManifest(func(m *manifest) { m.Overrides[0].SourceURL = "https://[invalid" }), wantErr: "valid https URL"},
		{name: "invalid review date", mutate: mutateManifest(func(m *manifest) { m.Overrides[0].ReviewedOn = "August 5" }), wantErr: "YYYY-MM-DD"},
		{name: "review interval over 30 days", mutate: mutateManifest(func(m *manifest) { m.Overrides[0].ReviewDueOn = "2026-09-05" }), wantErr: "exceeds 30 days"},
		{name: "invalid lifecycle data", mutate: func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, "api", "amazon-aurora-mysql.json"), []byte(`[{"cycle":"3","eol":42}]`), 0o600))
		}, wantErr: "unsupported value type"},
		{name: "API data is not an array", mutate: func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, "api", "amazon-aurora-mysql.json"), []byte(`null`), 0o600))
		}, wantErr: "top-level array"},
		{name: "overdue review warns", mutate: mutateManifest(func(m *manifest) { m.Overrides[0].ReviewDueOn = "2026-08-06" }), wantWarning: "review overdue"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyRepositoryFixture(t)
			if tt.mutate != nil {
				tt.mutate(t, root)
			}
			var warnings bytes.Buffer
			err := validateManifest(root, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC), &warnings)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Contains(t, warnings.String(), tt.wantWarning)
		})
	}
}

func TestRepositoryManifest(t *testing.T) {
	var warnings bytes.Buffer
	require.NoError(t, validateManifest(".", time.Now().UTC(), &warnings))
	if warnings.Len() > 0 {
		t.Log(strings.TrimSpace(warnings.String()))
	}
}

func mutateManifest(mutate func(*manifest)) func(*testing.T, string) {
	return func(t *testing.T, root string) {
		path := filepath.Join(root, "manifest.json")
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		var m manifest
		require.NoError(t, json.Unmarshal(data, &m))
		mutate(&m)
		data, err = json.Marshal(m)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0o600))
	}
}

func copyRepositoryFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, path := range []string{"manifest.json", "api/amazon-aurora-mysql.json", "api/amazon-opensearch.json"} {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		destination := filepath.Join(root, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0o700))
		require.NoError(t, os.WriteFile(destination, data, 0o600))
	}
	return root
}
