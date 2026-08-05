package endoflife

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sync/singleflight"

	"github.com/block/Version-Guard/pkg/types"
)

const (
	providerName = "endoflife-date-api"
	falseBool    = "false"
)

// Provider fetches EOL data for a single endoflife.date product.
//
// The product (e.g. "amazon-aurora-postgresql", "amazon-eks") and the
// schema adapter are both set at construction time from YAML-declared
// eol.product / eol.schema / eol.lifecycle. One Provider instance per
// resource keeps each product's cache and singleflight key isolated.
// Products with non-standard endoflife.date semantics should use the
// declarative lifecycle schema in YAML rather than adding a hardcoded
// product check in Provider.
//
//nolint:govet // field alignment sacrificed for readability
type Provider struct {
	mu       sync.RWMutex
	cache    map[string]*cachedVersions
	client   Client
	adapter  SchemaAdapter
	product  string // endoflife.date product identifier (e.g. "amazon-aurora-postgresql")
	cacheTTL time.Duration
	group    singleflight.Group // Prevents thundering herd on API calls
	logger   *slog.Logger
}

//nolint:govet // field alignment sacrificed for readability
type cachedVersions struct {
	versions  []*types.VersionLifecycle
	fetchedAt time.Time
}

// NewProvider creates a new endoflife.date EOL provider bound to a single
// product and schema. Empty schema defaults to "standard". Returns an
// error when schema is not a registered adapter — the loader also
// validates this so misconfiguration fails at startup rather than
// mid-scan.
func NewProvider(client Client, product, schema string, cacheTTL time.Duration, logger *slog.Logger) (*Provider, error) {
	return NewProviderWithLifecycle(client, product, schema, nil, cacheTTL, logger)
}

// NewProviderWithLifecycle creates a provider with an optional YAML
// lifecycle mapping. Non-nil lifecycle config selects the declarative
// adapter; otherwise schema names a built-in adapter such as "standard".
func NewProviderWithLifecycle(
	client Client,
	product string,
	schema string,
	lifecycle *DeclarativeLifecycleConfig,
	cacheTTL time.Duration,
	logger *slog.Logger,
) (*Provider, error) {
	if schema == "" {
		schema = SchemaStandard
	}

	var adapter SchemaAdapter
	if lifecycle != nil {
		declarativeAdapter, err := NewDeclarativeSchemaAdapter(lifecycle)
		if err != nil {
			return nil, errors.Wrapf(err, "endoflife provider for product %q", product)
		}
		adapter = declarativeAdapter
	} else {
		var err error
		adapter, err = GetSchemaAdapter(schema)
		if err != nil {
			return nil, errors.Wrapf(err, "endoflife provider for product %q", product)
		}
	}
	if cacheTTL == 0 {
		cacheTTL = 24 * time.Hour // Default: cache for 24 hours
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Provider{
		client:   client,
		adapter:  adapter,
		product:  product,
		cacheTTL: cacheTTL,
		cache:    make(map[string]*cachedVersions),
		logger:   logger,
	}, nil
}

// Name returns the name of this provider
func (p *Provider) Name() string {
	return providerName
}

// Engines returns the engines this provider serves. A Provider instance
// is bound to a single endoflife.date product, so the returned slice is
// effectively informational — it carries the product identifier so
// callers iterating over multiple providers can tell them apart.
func (p *Provider) Engines() []string {
	return []string{p.product}
}

// GetVersionLifecycle retrieves lifecycle information for a specific version
// of the provider's product. The engine argument is preserved as a label
// on the returned VersionLifecycle for downstream display; product
// resolution comes from p.product, set at construction time.
//
// Concurrency note: this function MUST NOT mutate the *VersionLifecycle
// pointers it gets back from ListAllVersions — those are shared across
// concurrent callers via the cache.
func (p *Provider) GetVersionLifecycle(ctx context.Context, engine, version string) (*types.VersionLifecycle, error) {
	engine = strings.ToLower(engine)
	version = strings.TrimSpace(version)

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
		cycleVersion := strings.TrimSpace(v.Version)
		if cycleVersion == version {
			return v, nil
		}
		if strings.HasPrefix(version, cycleVersion+".") && len(cycleVersion) > bestMatchLen {
			bestMatch = v
			bestMatchLen = len(cycleVersion)
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
	//
	return &types.VersionLifecycle{
		Version:     "", // Empty = unknown data, not unsupported version
		Engine:      engine,
		IsSupported: false,
		Source:      p.Name(),
		FetchedAt:   time.Now(),
	}, nil
}

// ListAllVersions retrieves all versions for the provider's product.
// The engine argument is preserved on the returned VersionLifecycle
// values for downstream display; it does not affect which product is
// queried (each Provider instance is bound to a single product at
// construction time).
func (p *Provider) ListAllVersions(ctx context.Context, engine string) ([]*types.VersionLifecycle, error) {
	// Normalize engine (used only as a label on returned VersionLifecycles)
	engine = strings.ToLower(engine)

	product := p.product

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
		result, err := p.client.GetProductCycles(ctx, product)
		if err != nil {
			// 404 (product not yet on endoflife.date — new product or
			// pending PR like aurora-mysql) is treated as an empty
			// cycles list so the caller emits UNKNOWN findings rather
			// than failing the scan. Cache the empty result with the
			// same TTL — singleflight only collapses *concurrent*
			// calls, so without caching here every serial lookup
			// would re-hit the upstream API for an unknown product.
			if errors.Is(err, ErrProductNotFound) {
				p.logger.WarnContext(ctx, "product not found on endoflife.date, caching empty lifecycle",
					"engine", engine,
					"product", product,
					"note", "This may be a new product or pending PR on endoflife.date")
				empty := []*types.VersionLifecycle{}
				p.mu.Lock()
				p.cache[cacheKey] = &cachedVersions{
					versions:  empty,
					fetchedAt: time.Now(),
				}
				p.mu.Unlock()
				return empty, nil
			}
			return nil, errors.Wrapf(err, "failed to fetch cycles for product %s", product)
		}

		// Convert to our types
		var versions []*types.VersionLifecycle
		for _, cycle := range result.Cycles {
			lifecycle, err := p.convertCycle(engine, product, cycle)
			if err != nil {
				// Skip cycles we can't parse, but log a warning
				p.logger.WarnContext(ctx, "failed to convert EOL cycle, skipping",
					"engine", engine,
					"product", product,
					"error", err)
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

// convertCycle delegates the cycle→VersionLifecycle conversion to the
// schema adapter selected at construction time, then overrides the
// caller-facing engine label and source.
//
// engine is the resource's engine name (e.g. "eks", "aurora-postgresql")
// — the adapter's notion of engine is internal and may be empty or a
// schema-specific placeholder, so we always overwrite it here. product
// is unused inside the wrapper but accepted for symmetry with logging
// and future per-product hooks.
func (p *Provider) convertCycle(engine, product string, cycle *ProductCycle) (*types.VersionLifecycle, error) {
	_ = product
	lifecycle, err := p.adapter.AdaptCycle(cycle)
	if err != nil {
		return nil, err
	}
	lifecycle.Engine = engine
	lifecycle.Source = p.Name()
	lifecycle.FetchedAt = time.Now()
	return lifecycle, nil
}

// anyToDateString extracts a date string from an any-typed field.
// endoflife.date returns EOL/Support as either a date string or a boolean.
func anyToDateString(v any) string {
	if val, ok := v.(string); ok && val != "" && val != "false" && val != "true" {
		return val
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
