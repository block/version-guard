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
		{name: "unsupported schema version", mutate: mutateManifest(func(m *manifest) { m.SchemaVersion = 2 }), wantErr: "schema_version must be 1"},
		{name: "missing required field", mutate: mutateManifest(func(m *manifest) { m.Overrides[0].Owner = "" }), wantErr: "owner is required"},
		{name: "duplicate product", mutate: mutateManifest(func(m *manifest) { m.Overrides = append(m.Overrides, m.Overrides[0]) }), wantErr: "duplicate product"},
		{name: "duplicate path", mutate: mutateManifest(func(m *manifest) { m.Overrides[1].Path = m.Overrides[0].Path }), wantErr: "duplicate path"},
		{name: "missing API file entry", mutate: func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, "api", "unlisted.json"), []byte("[]"), 0o600))
		}, wantErr: "has no manifest entry"},
		{name: "entry references missing file", mutate: func(t *testing.T, root string) {
			require.NoError(t, os.Remove(filepath.Join(root, "api", "amazon-aurora-mysql.json")))
		}, wantErr: "does not exist"},
		{name: "invalid source URL", mutate: mutateManifest(func(m *manifest) { m.Overrides[0].SourceURL = "http://example.com/source" }), wantErr: "must use https"},
		{name: "malformed source URL", mutate: mutateManifest(func(m *manifest) { m.Overrides[0].SourceURL = "https://[invalid" }), wantErr: "valid https URL"},
		{name: "invalid review date", mutate: mutateManifest(func(m *manifest) { m.Overrides[0].ReviewedOn = "August 5" }), wantErr: "YYYY-MM-DD"},
		{name: "review due before reviewed", mutate: mutateManifest(func(m *manifest) { m.Overrides[0].ReviewDueOn = "2026-08-04" }), wantErr: "before reviewed_on"},
		{name: "review interval over 30 days", mutate: mutateManifest(func(m *manifest) { m.Overrides[0].ReviewDueOn = "2026-09-05" }), wantErr: "exceeds 30 days"},
		{name: "path escapes API directory", mutate: mutateManifest(func(m *manifest) { m.Overrides[0].Path = "api/../manifest.json" }), wantErr: "direct api/"},
		{name: "nested API path", mutate: func(t *testing.T, root string) {
			nested := filepath.Join(root, "api", "nested", "amazon-aurora-mysql.json")
			require.NoError(t, os.MkdirAll(filepath.Dir(nested), 0o700))
			require.NoError(t, os.Rename(filepath.Join(root, "api", "amazon-aurora-mysql.json"), nested))
			mutateManifest(func(m *manifest) { m.Overrides[0].Path = "api/nested/amazon-aurora-mysql.json" })(t, root)
		}, wantErr: "direct api/"},
		{name: "non-JSON API path", mutate: func(t *testing.T, root string) {
			nonJSON := filepath.Join(root, "api", "amazon-aurora-mysql.txt")
			require.NoError(t, os.Rename(filepath.Join(root, "api", "amazon-aurora-mysql.json"), nonJSON))
			mutateManifest(func(m *manifest) { m.Overrides[0].Path = "api/amazon-aurora-mysql.txt" })(t, root)
		}, wantErr: "direct api/"},
		{name: "trailing manifest JSON", mutate: func(t *testing.T, root string) {
			path := filepath.Join(root, "manifest.json")
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			require.NoError(t, err)
			_, err = file.WriteString("\n{}")
			require.NoError(t, err)
			require.NoError(t, file.Close())
		}, wantErr: "multiple JSON values"},
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

func TestValidateManifestReviewDueDateBoundary(t *testing.T) {
	tests := []struct {
		name        string
		now         time.Time
		wantWarning string
	}{
		{name: "due date remains valid for full UTC day", now: time.Date(2026, 9, 4, 23, 59, 59, 0, time.UTC)},
		{name: "following UTC day warns", now: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), wantWarning: "review overdue"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var warnings bytes.Buffer
			require.NoError(t, validateManifest(copyRepositoryFixture(t), tt.now, &warnings))
			require.Equal(t, tt.wantWarning != "", strings.Contains(warnings.String(), "review overdue"))
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
