package override

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/block/Version-Guard/pkg/eol/endoflife"
)

const (
	manifestSchemaVersion = 1
	dateLayout            = "2006-01-02"
	maximumReviewInterval = 30 * 24 * time.Hour
)

type manifest struct {
	Overrides     []manifestOverride `json:"overrides"`
	SchemaVersion int                `json:"schema_version"`
}

type manifestOverride struct {
	Product     string `json:"product"`
	Path        string `json:"path"`
	Reason      string `json:"reason"`
	Owner       string `json:"owner"`
	SourceURL   string `json:"source_url"`
	ReviewedOn  string `json:"reviewed_on"`
	ReviewDueOn string `json:"review_due_on"`
}

func validateManifest(root string, now time.Time, warnings io.Writer) error {
	m, err := readManifest(filepath.Join(root, "manifest.json"))
	if err != nil {
		return err
	}
	if m.SchemaVersion != manifestSchemaVersion {
		return fmt.Errorf("schema_version must be %d", manifestSchemaVersion)
	}
	if warnings == nil {
		warnings = io.Discard
	}

	apiDirectory := filepath.Join(root, "api")
	apiInfo, err := os.Lstat(apiDirectory)
	if err != nil {
		return fmt.Errorf("stat API directory: %w", err)
	}
	if apiInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("API directory must not be a symlink")
	}
	if !apiInfo.IsDir() {
		return fmt.Errorf("API directory is not a directory")
	}

	products := make(map[string]struct{}, len(m.Overrides))
	paths := make(map[string]struct{}, len(m.Overrides))
	for index := range m.Overrides {
		override := &m.Overrides[index]
		if validationErr := validateOverride(root, override, now.UTC(), warnings, products, paths); validationErr != nil {
			return fmt.Errorf("override %d: %w", index, validationErr)
		}
	}

	apiFiles, err := filepath.Glob(filepath.Join(apiDirectory, "*.json"))
	if err != nil {
		return fmt.Errorf("list API files: %w", err)
	}
	for _, apiFile := range apiFiles {
		relative, err := filepath.Rel(root, apiFile)
		if err != nil {
			return fmt.Errorf("resolve API file %q: %w", apiFile, err)
		}
		relative = filepath.ToSlash(relative)
		if _, ok := paths[relative]; !ok {
			return fmt.Errorf("API file %q has no manifest entry", relative)
		}
	}
	return nil
}

func readManifest(path string) (*manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var m manifest
	if err := decoder.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &m, nil
}

//nolint:gocyclo // Validation intentionally reports the first field-specific policy violation.
func validateOverride(root string, override *manifestOverride, now time.Time, warnings io.Writer, products, paths map[string]struct{}) error {
	required := []struct {
		name  string
		value string
	}{
		{"product", override.Product}, {"path", override.Path}, {"reason", override.Reason},
		{"owner", override.Owner}, {"source_url", override.SourceURL},
		{"reviewed_on", override.ReviewedOn}, {"review_due_on", override.ReviewDueOn},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	if _, exists := products[override.Product]; exists {
		return fmt.Errorf("duplicate product %q", override.Product)
	}
	products[override.Product] = struct{}{}
	if _, exists := paths[override.Path]; exists {
		return fmt.Errorf("duplicate path %q", override.Path)
	}
	paths[override.Path] = struct{}{}

	if !strings.HasPrefix(override.SourceURL, "https://") {
		return fmt.Errorf("source_url must use https")
	}
	if _, err := parseHTTPSURL(override.SourceURL); err != nil {
		return err
	}
	reviewedOn, err := parseManifestDate("reviewed_on", override.ReviewedOn)
	if err != nil {
		return err
	}
	reviewDueOn, err := parseManifestDate("review_due_on", override.ReviewDueOn)
	if err != nil {
		return err
	}
	interval := reviewDueOn.Sub(reviewedOn)
	if interval < 0 {
		return fmt.Errorf("review_due_on is before reviewed_on")
	}
	if interval > maximumReviewInterval {
		return fmt.Errorf("review interval exceeds 30 days")
	}
	if !now.Before(reviewDueOn.AddDate(0, 0, 1)) {
		fmt.Fprintf(warnings, "warning: review overdue for %s (due %s)\n", override.Product, override.ReviewDueOn)
	}

	cleanPath := filepath.ToSlash(filepath.Clean(override.Path))
	filename := strings.TrimPrefix(cleanPath, "api/")
	if cleanPath != override.Path || filename == cleanPath || filename == "" ||
		strings.ContainsAny(filename, `/\`) || filepath.Ext(filename) != ".json" {
		return fmt.Errorf("path %q must be a direct api/<filename>.json path", override.Path)
	}
	if strings.TrimSuffix(filename, ".json") != override.Product {
		return fmt.Errorf("path filename must match product %q", override.Product)
	}
	fullPath := filepath.Join(root, filepath.FromSlash(cleanPath))
	info, err := os.Lstat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("path %q does not exist", override.Path)
		}
		return fmt.Errorf("stat path %q: %w", override.Path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path %q must not be a symlink", override.Path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path %q is not a regular file", override.Path)
	}
	return validateAPIFile(fullPath)
}

func parseHTTPSURL(raw string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("source_url must be a valid https URL")
	}
	return parsed, nil
}

func parseManifestDate(name, value string) (time.Time, error) {
	parsed, err := time.Parse(dateLayout, value)
	if err != nil || parsed.Format(dateLayout) != value {
		return time.Time{}, fmt.Errorf("%s must use YYYY-MM-DD", name)
	}
	return parsed, nil
}

func validateAPIFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open API file %q: %w", path, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var cycles []*endoflife.ProductCycle
	if err := decoder.Decode(&cycles); err != nil {
		return fmt.Errorf("decode API file %q: %w", path, err)
	}
	if cycles == nil {
		return fmt.Errorf("API file %q must contain a top-level array", path)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return fmt.Errorf("decode API file %q: %w", path, err)
	}
	for index, cycle := range cycles {
		if err := endoflife.ValidateProductCycle(cycle); err != nil {
			return fmt.Errorf("API file %q cycle %d: %w", path, index, err)
		}
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}
