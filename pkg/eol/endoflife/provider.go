package endoflife

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sync/singleflight"

	"github.com/block/Version-Guard/pkg/types"
)

// ProductMapping maps internal engine names to endoflife.date product identifiers
//
// WARNING: This provider uses STANDARD endoflife.date field semantics:
//   - cycle.EOL → true end of life date
//   - cycle.Support → end of standard support date
//
// Some AWS products (e.g., EKS) use NON-STANDARD schemas on endoflife.date
// and MUST use dedicated providers (e.g., EKSEOLProvider) instead of this generic provider.
// These products are listed here but blocked by ProductsWithNonStandardSchema below.
var ProductMapping = map[string]string{
	"kubernetes": "amazon-eks",
	"k8s":        "amazon-eks",
	"eks":        "amazon-eks",

	"postgres":           "amazon-rds-postgresql",
	"postgresql":         "amazon-rds-postgresql",
	"mysql":              "amazon-rds-mysql",
	"aurora-postgresql":  "amazon-aurora-postgresql",
	"redis":              "amazon-elasticache-redis",
	"elasticache-redis":  "amazon-elasticache-redis",
	"valkey":             "valkey",
	"elasticache-valkey": "valkey",
	// Note: aurora-mysql is NOT mapped because endoflife.date has no
	// amazon-aurora-mysql product. Aurora MySQL uses its own 3.x versioning
	// that doesn't match amazon-rds-mysql cycles (8.0, 5.7). Needs AWS RDS API.
}

// ProductsWithNonStandardSchema lists products that MUST NOT use this generic provider
// because they use non-standard field semantics on endoflife.date.
// The provider will return an error if these products are requested.
//
// Note on EKS: endoflife.date's "eol" field for EKS means end of standard support
// (not true EOL), and "extendedSupport" is the true EOL. This is handled correctly
// by convertCycle which maps eol→EOLDate and extendedSupport→ExtendedSupportEnd.
var ProductsWithNonStandardSchema = []string{}

const (
	providerName = "endoflife-date-api"
	falseBool    = "false"
)

// Provider fetches EOL data from endoflife.date API
//
//nolint:govet // field alignment sacrificed for readability
type Provider struct {
	mu       sync.RWMutex
	cache    map[string]*cachedVersions
	client   Client
	cacheTTL time.Duration
	group    singleflight.Group // Prevents thundering herd on API calls
}

//nolint:govet // field alignment sacrificed for readability
type cachedVersions struct {
	versions  []*types.VersionLifecycle
	fetchedAt time.Time
}

// NewProvider creates a new endoflife.date EOL provider
func NewProvider(client Client, cacheTTL time.Duration) *Provider {
	if cacheTTL == 0 {
		cacheTTL = 24 * time.Hour // Default: cache for 24 hours
	}

	return &Provider{
		client:   client,
		cacheTTL: cacheTTL,
		cache:    make(map[string]*cachedVersions),
	}
}

// Name returns the name of this provider
func (p *Provider) Name() string {
	return providerName
}

// Engines returns the list of supported engines
func (p *Provider) Engines() []string {
	engines := make([]string, 0, len(ProductMapping))
	for engine := range ProductMapping {
		engines = append(engines, engine)
	}
	return engines
}

// GetVersionLifecycle retrieves lifecycle information for a specific version
func (p *Provider) GetVersionLifecycle(ctx context.Context, engine, version string) (*types.VersionLifecycle, error) {
	// Normalize engine name
	engine = strings.ToLower(engine)
	if !p.supportsEngine(engine) {
		return nil, fmt.Errorf("unsupported engine: %s", engine)
	}

	// Normalize version format
	version = normalizeVersion(engine, version)

	// Fetch all versions
	versions, err := p.ListAllVersions(ctx, engine)
	if err != nil {
		return nil, err
	}

	// Find the specific version — try exact match first, then prefix match.
	// endoflife.date uses major.minor cycles (e.g., "8.0", "7") while Wiz
	// reports full versions (e.g., "8.0.35", "7.1.0").
	var bestMatch *types.VersionLifecycle
	bestMatchLen := 0
	for _, v := range versions {
		normalizedV := normalizeVersion(engine, v.Version)
		if normalizedV == version {
			return v, nil
		}
		if strings.HasPrefix(version, normalizedV+".") && len(normalizedV) > bestMatchLen {
			bestMatch = v
			bestMatchLen = len(normalizedV)
		}
	}
	if bestMatch != nil {
		return bestMatch, nil
	}

	// Version not found - return unknown lifecycle (empty Version signals missing data)
	//
	// Design Decision: Return lifecycle with empty Version rather than error
	// Rationale:
	//   - Maintains observability: Resource tracked with UNKNOWN status vs lost entirely
	//   - Graceful degradation: Workflow continues during partial API outages
	//   - Policy decides: EOL provider fetches data, policy layer interprets "unknown"
	//
	// Alternative (rejected): Return error - would cause workflow to skip resource,
	// losing visibility into resources with incomplete EOL data coverage.
	return &types.VersionLifecycle{
		Version:     "", // Empty = unknown data, not unsupported version
		Engine:      engine,
		IsSupported: false,
		Source:      p.Name(),
		FetchedAt:   time.Now(),
	}, nil
}

// ListAllVersions retrieves all versions for an engine
func (p *Provider) ListAllVersions(ctx context.Context, engine string) ([]*types.VersionLifecycle, error) {
	// Normalize engine
	engine = strings.ToLower(engine)

	// Get product identifier
	product, ok := ProductMapping[engine]
	if !ok {
		return nil, fmt.Errorf("unsupported engine: %s", engine)
	}

	// Guard against products with non-standard schemas
	// These products interpret endoflife.date fields differently and need dedicated providers
	for _, blockedProduct := range ProductsWithNonStandardSchema {
		if product == blockedProduct {
			return nil, fmt.Errorf(
				"engine %s (product: %s) uses non-standard endoflife.date schema and cannot use generic provider; use dedicated provider instead (e.g., EKSEOLProvider)",
				engine, product,
			)
		}
	}

	// Use product as cache key
	cacheKey := product

	// Check cache first (fast path)
	p.mu.RLock()
	if cached, found := p.cache[cacheKey]; found {
		if time.Since(cached.fetchedAt) < p.cacheTTL {
			versions := cached.versions
			p.mu.RUnlock()
			return versions, nil
		}
	}
	p.mu.RUnlock()

	// Cache miss or expired - use singleflight to prevent thundering herd
	result, err, _ := p.group.Do(cacheKey, func() (interface{}, error) {
		// Fetch from endoflife.date API (only one goroutine executes this)
		cycles, err := p.client.GetProductCycles(ctx, product)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to fetch cycles for product %s", product)
		}

		// Convert to our types
		var versions []*types.VersionLifecycle
		for _, cycle := range cycles {
			lifecycle, err := p.convertCycle(engine, product, cycle)
			if err != nil {
				// Skip cycles we can't parse, but log a warning
				// TODO: wire through proper structured logger
				continue
			}
			versions = append(versions, lifecycle)
		}

		// Cache the result
		p.mu.Lock()
		p.cache[cacheKey] = &cachedVersions{
			versions:  versions,
			fetchedAt: time.Now(),
		}
		p.mu.Unlock()

		return versions, nil
	})

	if err != nil {
		return nil, err
	}

	versions, ok := result.([]*types.VersionLifecycle)
	if !ok {
		return nil, errors.New("failed to convert result to VersionLifecycle slice")
	}
	return versions, nil
}

// convertCycle converts a ProductCycle to our VersionLifecycle type
//
// Field Mapping (STANDARD endoflife.date schema):
//   - cycle.ReleaseDate → ReleaseDate
//   - cycle.Support → DeprecationDate (end of standard support)
//   - cycle.EOL → EOLDate (true end of life)
//   - cycle.ExtendedSupport → ExtendedSupportEnd
//
// WARNING: This assumes STANDARD field semantics. Products with non-standard schemas
// (e.g., amazon-eks where cycle.EOL means "end of standard support", not true EOL)
// should be blocked by ListAllVersions and use dedicated providers instead.
func (p *Provider) convertCycle(engine, product string, cycle *ProductCycle) (*types.VersionLifecycle, error) {
	version := cycle.Cycle

	lifecycle := &types.VersionLifecycle{
		Version:   version,
		Engine:    engine,
		Source:    p.Name(),
		FetchedAt: time.Now(),
	}

	// Parse release date
	if cycle.ReleaseDate != "" {
		if releaseDate, err := parseDate(cycle.ReleaseDate); err == nil {
			lifecycle.ReleaseDate = &releaseDate
		}
	}

	// Parse EOL date (STANDARD semantics: true end of life)
	var eolDate *time.Time
	if dateStr := anyToDateString(cycle.EOL); dateStr != "" {
		if parsed, err := parseDate(dateStr); err == nil {
			eolDate = &parsed
			lifecycle.EOLDate = eolDate
		}
	}

	// Parse support date (STANDARD semantics: end of standard support)
	var supportDate *time.Time
	if dateStr := anyToDateString(cycle.Support); dateStr != "" {
		if parsed, err := parseDate(dateStr); err == nil {
			supportDate = &parsed
			lifecycle.DeprecationDate = supportDate
		}
	}

	// Parse extended support
	var extendedSupportDate *time.Time
	if cycle.ExtendedSupport != nil {
		switch v := cycle.ExtendedSupport.(type) {
		case string:
			if v != "" && v != falseBool {
				if parsed, err := parseDate(v); err == nil {
					extendedSupportDate = &parsed
					lifecycle.ExtendedSupportEnd = extendedSupportDate
				}
			}
		case bool:
			// If boolean true, use EOL date as extended support end
			if v && eolDate != nil {
				extendedSupportDate = eolDate
				lifecycle.ExtendedSupportEnd = eolDate
			}
		}
	}

	// Determine lifecycle status based on dates.
	// For products like EKS, the "support" field is absent and "eol" means
	// end of standard support while "extendedSupport" is the true end of life.
	// We treat eolDate as the standard support boundary when supportDate is nil.
	now := time.Now()

	// Resolve the effective standard-support-end date
	standardEnd := supportDate
	if standardEnd == nil {
		standardEnd = eolDate
	}

	// Check extended support window first — must come before the EOL check
	// so that resources in extended support get YELLOW, not RED.
	if extendedSupportDate != nil && standardEnd != nil && now.After(*standardEnd) {
		if now.Before(*extendedSupportDate) {
			// In extended support window
			lifecycle.IsSupported = true
			lifecycle.IsExtendedSupport = true
			lifecycle.IsDeprecated = true
		} else {
			// Past extended support — truly EOL
			lifecycle.IsEOL = true
			lifecycle.IsSupported = false
			lifecycle.IsDeprecated = true
		}
		return lifecycle, nil
	}

	// Past EOL with no extended support available
	if eolDate != nil && now.After(*eolDate) {
		lifecycle.IsEOL = true
		lifecycle.IsSupported = false
		lifecycle.IsDeprecated = true
		return lifecycle, nil
	}

	// Past standard support but no extended support info
	if supportDate != nil && now.After(*supportDate) {
		lifecycle.IsDeprecated = true
		lifecycle.IsSupported = false
		if eolDate != nil && now.Before(*eolDate) {
			lifecycle.IsEOL = false
		}
		return lifecycle, nil
	}

	// Still in standard support
	lifecycle.IsSupported = true
	lifecycle.IsDeprecated = false
	lifecycle.IsEOL = false

	return lifecycle, nil
}

// supportsEngine checks if the engine is supported by this provider
func (p *Provider) supportsEngine(engine string) bool {
	engine = strings.ToLower(engine)
	_, ok := ProductMapping[engine]
	return ok
}

// normalizeVersion normalizes version strings for comparison
func normalizeVersion(engine, version string) string {
	version = strings.TrimSpace(version)

	// Handle kubernetes/EKS versions
	if engine == "kubernetes" || engine == "k8s" || engine == "eks" {
		version = strings.TrimPrefix(version, "k8s-")
		version = strings.TrimPrefix(version, "kubernetes-")
		return version
	}

	// For other engines, return as-is
	return version
}

// anyToDateString extracts a date string from an any-typed field.
// endoflife.date returns EOL/Support as either a date string or a boolean.
func anyToDateString(v any) string {
	switch val := v.(type) {
	case string:
		if val != "" && val != "false" && val != "true" {
			return val
		}
	}
	return ""
}

// parseDate parses date strings from endoflife.date API
// Supports formats: YYYY-MM-DD, boolean "true"/"false"
func parseDate(dateStr string) (time.Time, error) {
	dateStr = strings.TrimSpace(dateStr)

	// Handle boolean values
	if dateStr == "true" || dateStr == "false" {
		return time.Time{}, errors.New("boolean value not a date")
	}

	// Try YYYY-MM-DD format
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "failed to parse date: %s", dateStr)
	}

	return t, nil
}
