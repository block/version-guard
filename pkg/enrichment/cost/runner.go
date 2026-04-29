package cost

import (
	"context"
	"time"

	"github.com/block/Version-Guard/pkg/types"
)

const (
	defaultMonthlyHours = 730
	defaultAnnualHours  = 8760
)

// CostCheck is implemented by pluggable finding-level cost checks.
type CostCheck interface {
	ID() string
	AppliesTo(finding *types.Finding) bool
	Estimate(ctx context.Context, input CostInput) (*types.CostEstimate, error)
}

// CostInput is passed to each cost check for a single finding.
type CostInput struct {
	Snapshot     *types.Snapshot
	Finding      *types.Finding
	AsOf         time.Time
	MonthlyHours float64
	AnnualHours  float64
}

// Runner applies registered cost checks to a snapshot.
type Runner struct {
	asOf         time.Time
	checks       []CostCheck
	monthlyHours float64
	annualHours  float64
}

// Option configures a Runner.
type Option func(*Runner)

// WithChecks registers checks on a Runner.
func WithChecks(checks ...CostCheck) Option {
	return func(r *Runner) {
		r.checks = append(r.checks, checks...)
	}
}

// WithAsOf sets the time used for projected/current estimates.
func WithAsOf(asOf time.Time) Option {
	return func(r *Runner) {
		r.asOf = asOf
	}
}

// WithHours sets the monthly and annual estimate windows.
func WithHours(monthlyHours, annualHours float64) Option {
	return func(r *Runner) {
		r.monthlyHours = monthlyHours
		r.annualHours = annualHours
	}
}

// NewRunner creates a cost enrichment runner.
func NewRunner(opts ...Option) *Runner {
	r := &Runner{
		asOf:         time.Now(),
		monthlyHours: defaultMonthlyHours,
		annualHours:  defaultAnnualHours,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Enrich applies all applicable checks and rebuilds the snapshot cost summary.
func (r *Runner) Enrich(ctx context.Context, snapshot *types.Snapshot) (*types.Snapshot, error) {
	if snapshot == nil {
		return nil, nil
	}
	for _, findings := range snapshot.FindingsByType {
		for _, finding := range findings {
			for _, check := range r.checks {
				if !check.AppliesTo(finding) {
					continue
				}
				estimate, err := check.Estimate(ctx, CostInput{
					Snapshot:     snapshot,
					Finding:      finding,
					AsOf:         r.asOf,
					MonthlyHours: r.monthlyHours,
					AnnualHours:  r.annualHours,
				})
				if err != nil {
					estimate = &types.CostEstimate{
						CheckID:       check.ID(),
						Status:        types.CostEstimateStatusInsufficientData,
						MissingInputs: []string{"estimation_error"},
						Assumptions:   []string{err.Error()},
					}
				}
				if estimate != nil {
					if estimate.CheckID == "" {
						estimate.CheckID = check.ID()
					}
					finding.CostEstimates = append(finding.CostEstimates, *estimate)
				}
			}
		}
	}
	snapshot.Summary.CostSummary = BuildSummary(snapshot)
	return snapshot, nil
}

// BuildSummary aggregates cost estimates across all findings in a snapshot.
func BuildSummary(snapshot *types.Snapshot) *types.CostSummary {
	summary := &types.CostSummary{
		ByService: make(map[string]*types.CostSummaryBucket),
		ByBrand:   make(map[string]*types.CostSummaryBucket),
		ByAccount: make(map[string]*types.CostSummaryBucket),
		ByCheckID: make(map[string]*types.CostSummaryBucket),
	}
	if snapshot == nil {
		return summary
	}
	for _, findings := range snapshot.FindingsByType {
		for _, finding := range findings {
			for i := range finding.CostEstimates {
				estimate := &finding.CostEstimates[i]
				addEstimateToSummary(summary, estimate)
				if finding.Service != "" {
					addEstimateToBucket(getBucket(summary.ByService, finding.Service), estimate)
				}
				if brand := findingExtraValue(finding, "brand"); brand != "" {
					addEstimateToBucket(getBucket(summary.ByBrand, brand), estimate)
				}
				if accountID := findingExtraValue(finding, "account_id", "cloudAccount.externalId", "account", "subscription_external_id"); accountID != "" {
					addEstimateToBucket(getBucket(summary.ByAccount, accountID), estimate)
				}
				if estimate.CheckID != "" {
					addEstimateToBucket(getBucket(summary.ByCheckID, estimate.CheckID), estimate)
				}
			}
		}
	}
	return summary
}

func findingExtraValue(finding *types.Finding, keys ...string) string {
	if finding == nil {
		return ""
	}
	for _, key := range keys {
		if value := finding.Extra[key]; value != "" {
			return value
		}
	}
	return ""
}

func getBucket(m map[string]*types.CostSummaryBucket, key string) *types.CostSummaryBucket {
	bucket, ok := m[key]
	if !ok {
		bucket = &types.CostSummaryBucket{}
		m[key] = bucket
	}
	return bucket
}

func addEstimateToSummary(summary *types.CostSummary, estimate *types.CostEstimate) {
	switch estimate.Status {
	case types.CostEstimateStatusAvailable:
		summary.AvailableCount++
	case types.CostEstimateStatusInsufficientData:
		summary.InsufficientDataCount++
	case types.CostEstimateStatusRateUnavailable:
		summary.RateUnavailableCount++
	case types.CostEstimateStatusNotApplicable:
		summary.NotApplicableCount++
	}
	switch estimate.Basis {
	case types.CostEstimateBasisProjected:
		summary.ProjectedMonthlyUSD += estimate.MonthlyUSD
		summary.ProjectedAnnualUSD += estimate.AnnualUSD
	default:
		summary.CurrentMonthlyUSD += estimate.MonthlyUSD
		summary.CurrentAnnualUSD += estimate.AnnualUSD
	}
}

func addEstimateToBucket(bucket *types.CostSummaryBucket, estimate *types.CostEstimate) {
	switch estimate.Status {
	case types.CostEstimateStatusAvailable:
		bucket.AvailableCount++
	case types.CostEstimateStatusInsufficientData:
		bucket.InsufficientDataCount++
	case types.CostEstimateStatusRateUnavailable:
		bucket.RateUnavailableCount++
	case types.CostEstimateStatusNotApplicable:
		bucket.NotApplicableCount++
	}
	switch estimate.Basis {
	case types.CostEstimateBasisProjected:
		bucket.ProjectedMonthlyUSD += estimate.MonthlyUSD
		bucket.ProjectedAnnualUSD += estimate.AnnualUSD
	default:
		bucket.CurrentMonthlyUSD += estimate.MonthlyUSD
		bucket.CurrentAnnualUSD += estimate.AnnualUSD
	}
}
