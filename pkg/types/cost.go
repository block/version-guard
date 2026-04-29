package types

import "time"

// CostEstimateStatus is the enrichment status for one cost check.
type CostEstimateStatus string

const (
	CostEstimateStatusAvailable        CostEstimateStatus = "available"
	CostEstimateStatusInsufficientData CostEstimateStatus = "insufficient_data"
	CostEstimateStatusRateUnavailable  CostEstimateStatus = "rate_unavailable"
	CostEstimateStatusNotApplicable    CostEstimateStatus = "not_applicable"
)

// CostEstimateBasis identifies whether an estimate is for current or future spend.
type CostEstimateBasis string

const (
	CostEstimateBasisCurrent   CostEstimateBasis = "current"
	CostEstimateBasisProjected CostEstimateBasis = "projected"
)

// LifecycleDetails preserves structured lifecycle data on findings so
// downstream enrichment can reason about support windows without re-fetching EOL data.
type LifecycleDetails struct {
	StandardSupportEnd *time.Time `json:"standard_support_end,omitempty"`
	EOLDate            *time.Time `json:"eol_date,omitempty"`
	ExtendedSupportEnd *time.Time `json:"extended_support_end,omitempty"`
	ReleaseDate        *time.Time `json:"release_date,omitempty"`
	FetchedAt          time.Time  `json:"fetched_at,omitempty"`
	Version            string     `json:"version,omitempty"`
	Engine             string     `json:"engine,omitempty"`
	Source             string     `json:"source,omitempty"`
	IsSupported        bool       `json:"is_supported"`
	IsDeprecated       bool       `json:"is_deprecated"`
	IsExtendedSupport  bool       `json:"is_extended_support"`
	IsEOL              bool       `json:"is_eol"`
}

// LifecycleDetailsFromVersionLifecycle converts EOL provider output into
// finding-level lifecycle metadata.
func LifecycleDetailsFromVersionLifecycle(lifecycle *VersionLifecycle) *LifecycleDetails {
	if lifecycle == nil {
		return nil
	}
	standardSupportEnd := lifecycle.DeprecationDate
	if standardSupportEnd == nil && lifecycle.ExtendedSupportEnd != nil {
		standardSupportEnd = lifecycle.EOLDate
	}
	return &LifecycleDetails{
		ReleaseDate:        lifecycle.ReleaseDate,
		StandardSupportEnd: standardSupportEnd,
		EOLDate:            lifecycle.EOLDate,
		ExtendedSupportEnd: lifecycle.ExtendedSupportEnd,
		FetchedAt:          lifecycle.FetchedAt,
		Version:            lifecycle.Version,
		Engine:             lifecycle.Engine,
		Source:             lifecycle.Source,
		IsSupported:        lifecycle.IsSupported,
		IsDeprecated:       lifecycle.IsDeprecated,
		IsExtendedSupport:  lifecycle.IsExtendedSupport,
		IsEOL:              lifecycle.IsEOL,
	}
}

// CostEstimate is an optional finding-level cost annotation produced by an enricher.
//
//nolint:govet // field alignment sacrificed for readability
type CostEstimate struct {
	PricingEffectiveDate *time.Time         `json:"pricing_effective_date,omitempty"`
	CheckID              string             `json:"check_id"`
	Status               CostEstimateStatus `json:"status"`
	EstimateType         string             `json:"estimate_type,omitempty"`
	Basis                CostEstimateBasis  `json:"basis,omitempty"`
	Currency             string             `json:"currency,omitempty"`
	Unit                 string             `json:"unit,omitempty"`
	RatePerUnitHour      float64            `json:"rate_per_unit_hour,omitempty"`
	BillableUnitCount    float64            `json:"billable_unit_count,omitempty"`
	HourlyUSD            float64            `json:"hourly_usd,omitempty"`
	MonthlyUSD           float64            `json:"monthly_usd,omitempty"`
	AnnualUSD            float64            `json:"annual_usd,omitempty"`
	PricingRegion        string             `json:"pricing_region,omitempty"`
	PricingYear          string             `json:"pricing_year,omitempty"`
	PricingSource        string             `json:"pricing_source,omitempty"`
	Confidence           string             `json:"confidence,omitempty"`
	Assumptions          []string           `json:"assumptions,omitempty"`
	MissingInputs        []string           `json:"missing_inputs,omitempty"`
}

// CostSummary aggregates cost estimates across a snapshot.
type CostSummary struct {
	ByService             map[string]*CostSummaryBucket `json:"by_service,omitempty"`
	ByBrand               map[string]*CostSummaryBucket `json:"by_brand,omitempty"`
	ByAccount             map[string]*CostSummaryBucket `json:"by_account,omitempty"`
	ByCheckID             map[string]*CostSummaryBucket `json:"by_check_id,omitempty"`
	CurrentMonthlyUSD     float64                       `json:"current_monthly_usd,omitempty"`
	CurrentAnnualUSD      float64                       `json:"current_annual_usd,omitempty"`
	ProjectedMonthlyUSD   float64                       `json:"projected_monthly_usd,omitempty"`
	ProjectedAnnualUSD    float64                       `json:"projected_annual_usd,omitempty"`
	AvailableCount        int                           `json:"available_count,omitempty"`
	InsufficientDataCount int                           `json:"insufficient_data_count,omitempty"`
	RateUnavailableCount  int                           `json:"rate_unavailable_count,omitempty"`
	NotApplicableCount    int                           `json:"not_applicable_count,omitempty"`
}

// CostSummaryBucket aggregates cost estimates for one grouping key.
type CostSummaryBucket struct {
	CurrentMonthlyUSD     float64 `json:"current_monthly_usd,omitempty"`
	CurrentAnnualUSD      float64 `json:"current_annual_usd,omitempty"`
	ProjectedMonthlyUSD   float64 `json:"projected_monthly_usd,omitempty"`
	ProjectedAnnualUSD    float64 `json:"projected_annual_usd,omitempty"`
	AvailableCount        int     `json:"available_count,omitempty"`
	InsufficientDataCount int     `json:"insufficient_data_count,omitempty"`
	RateUnavailableCount  int     `json:"rate_unavailable_count,omitempty"`
	NotApplicableCount    int     `json:"not_applicable_count,omitempty"`
}
