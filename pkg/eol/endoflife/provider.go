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
var ProductMapping = map[string]string{
	"kubernetes":         "amazon-eks",
	"k8s":                "amazon-eks",
	"eks":                "amazon-eks",
	"postgres":           "amazon-rds-postgresql",
	"postgresql":         "amazon-rds-postgresql",
	"mysql":              "amazon-rds-mysql",
	"aurora-mysql":       "amazon-rds-mysql",
	"aurora-postgresql":  "amazon-rds-postgresql",
	"redis":              "amazon-elasticache-redis",
	"elasticache-redis":  "amazon-elasticache-redis",
	"valkey":             "valkey",
	"elasticache-valkey": "valkey",
}

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
	return "endoflife-date-api"
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

	// Find the specific version
	for _, v := range versions {
		normalizedV := normalizeVersion(engine, v.Version)
		if normalizedV == version {
			return v, nil
		}
	}

	// Version not found - return an unknown lifecycle
	// Version not found - return unknown lifecycle (empty Version signals missing data)
	// Policy will classify as UNKNOWN (data gap) rather than RED/YELLOW (user issue)
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

	// Use product as cache key
	cacheKey := product

	// Check cache first (fast path)
	p.mu.RLock()
	if cached, ok := p.cache[cacheKey]; ok {
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

	return result.([]*types.VersionLifecycle), nil
}

// convertCycle converts a ProductCycle to our VersionLifecycle type
func (p *Provider) convertCycle(engine, product string, cycle *ProductCycle) (*types.VersionLifecycle, error) {
	version := cycle.Cycle

	// Add engine-specific prefix for consistency
	if engine == "kubernetes" || engine == "k8s" || engine == "eks" {
		if !strings.HasPrefix(version, "k8s-") {
			version = "k8s-" + version
		}
	}

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

	// Parse EOL date
	var eolDate *time.Time
	if cycle.EOL != "" && cycle.EOL != "false" {
		if parsed, err := parseDate(cycle.EOL); err == nil {
			eolDate = &parsed
			lifecycle.EOLDate = eolDate
		}
	}

	// Parse support date (end of standard support)
	var supportDate *time.Time
	if cycle.Support != "" && cycle.Support != "false" {
		if parsed, err := parseDate(cycle.Support); err == nil {
			supportDate = &parsed
			lifecycle.DeprecationDate = supportDate
		}
	}

	// Parse extended support
	var extendedSupportDate *time.Time
	if cycle.ExtendedSupport != nil {
		switch v := cycle.ExtendedSupport.(type) {
		case string:
			if v != "" && v != "false" {
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

	// Determine lifecycle status based on dates
	now := time.Now()

	// If we have an EOL date and we're past it, mark as EOL
	if eolDate != nil && now.After(*eolDate) {
		lifecycle.IsEOL = true
		lifecycle.IsSupported = false
		lifecycle.IsDeprecated = true
		return lifecycle, nil
	}

	// If we have extended support end and we're past standard support
	if extendedSupportDate != nil && supportDate != nil && now.After(*supportDate) {
		if now.Before(*extendedSupportDate) {
			// In extended support window
			lifecycle.IsSupported = true
			lifecycle.IsExtendedSupport = true
			lifecycle.IsDeprecated = true
		} else {
			// Past extended support
			lifecycle.IsEOL = true
			lifecycle.IsSupported = false
			lifecycle.IsDeprecated = true
		}
		return lifecycle, nil
	}

	// If we're past support date but no extended support info
	if supportDate != nil && now.After(*supportDate) {
		lifecycle.IsDeprecated = true
		lifecycle.IsSupported = false
		// If we have EOL date, use it; otherwise mark as deprecated but not EOL
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
