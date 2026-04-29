package aurora

import (
	"context"
	"fmt"
	"strings"
	"time"

	cost "github.com/block/Version-Guard/pkg/enrichment/cost"
	costaws "github.com/block/Version-Guard/pkg/enrichment/cost/aws"
	"github.com/block/Version-Guard/pkg/types"
)

const (
	MySQLExtendedSupportCheckID = "aws.aurora_mysql.extended_support_surcharge"
	estimateType                = "aws_aurora_mysql_extended_support_surcharge"
	pricingYearOneTwo           = "Year 1, Year 2"
	pricingYearThree            = "Year 3"
	projectionWindowDays        = 90
)

// CostInput aliases the generic cost input for package-level tests and callers.
type CostInput = cost.CostInput

type priceSource interface {
	FindRate(ctx context.Context, query costaws.RateQuery) (*costaws.PriceListRow, error)
	SourceName() string
}

// MySQLExtendedSupportCheck estimates Aurora MySQL Extended Support surcharge.
type MySQLExtendedSupportCheck struct {
	instances    *InstanceIndex
	priceSource  priceSource
	vcpuResolver *costaws.VCPUResolver
}

// NewMySQLExtendedSupportCheck creates the Aurora MySQL surcharge check.
func NewMySQLExtendedSupportCheck(
	instances *InstanceIndex,
	priceSource priceSource,
	vcpuResolver *costaws.VCPUResolver,
) *MySQLExtendedSupportCheck {
	return &MySQLExtendedSupportCheck{
		instances:    instances,
		priceSource:  priceSource,
		vcpuResolver: vcpuResolver,
	}
}

// ID returns the stable check ID.
func (c *MySQLExtendedSupportCheck) ID() string {
	return MySQLExtendedSupportCheckID
}

// AppliesTo returns true for Aurora MySQL findings.
func (c *MySQLExtendedSupportCheck) AppliesTo(finding *types.Finding) bool {
	if finding == nil {
		return false
	}
	resourceType := strings.ToLower(string(finding.ResourceType))
	return finding.Engine == "aurora-mysql" &&
		(resourceType == "aurora-mysql" || resourceType == "aurora")
}

// Estimate estimates the Extended Support surcharge for one Aurora MySQL cluster.
func (c *MySQLExtendedSupportCheck) Estimate(ctx context.Context, input cost.CostInput) (*types.CostEstimate, error) {
	finding := input.Finding
	if finding == nil || finding.LifecycleDetails == nil {
		return nil, nil
	}

	basis, ok := estimateBasis(finding.LifecycleDetails, input.AsOf)
	if !ok {
		return nil, nil
	}

	accountID, region, clusterID := clusterJoinParts(finding)
	missing := missingJoinInputs(accountID, region, clusterID)
	if len(missing) > 0 {
		return insufficientEstimate(missing), nil
	}

	instances := c.instances.Find(accountID, region, clusterID)
	if len(instances) == 0 {
		return insufficientEstimate([]string{"aurora_instance_rows"}), nil
	}

	totalVCPU := 0
	for _, instance := range instances {
		if !isBillableStatus(instance.Status) {
			continue
		}
		if instance.DBInstanceClass == "" {
			return insufficientEstimate([]string{"db_instance_class"}), nil
		}
		if isServerlessClass(instance.DBInstanceClass) {
			return insufficientEstimate([]string{"serverless_acu_usage"}), nil
		}
		vcpu, err := c.vcpuResolver.Resolve(instance.DBInstanceClass)
		if err != nil {
			return insufficientEstimate([]string{"vcpu_count"}), nil
		}
		totalVCPU += vcpu
	}
	if totalVCPU == 0 {
		return insufficientEstimate([]string{"billable_aurora_instances"}), nil
	}

	auroraMajor, engineMajor := normalizeAuroraMySQLMajor(finding.CurrentVersion, finding.LifecycleDetails.Version)
	if auroraMajor == "" || engineMajor == "" {
		return insufficientEstimate([]string{"aurora_mysql_major_version"}), nil
	}

	pricingYear := extendedSupportPricingYear(finding.LifecycleDetails.StandardSupportEnd, input.AsOf)
	rate, err := c.priceSource.FindRate(ctx, costaws.RateQuery{
		RegionCode:                 region,
		DatabaseEngine:             "Aurora MySQL",
		EngineMajorVersion:         engineMajor,
		Unit:                       "vCPU-hour",
		ExtendedSupportPricingYear: pricingYear,
		UsageTypeContains:          "AuroraMySQL" + auroraMajor,
	})
	if err != nil {
		return &types.CostEstimate{
			CheckID:       c.ID(),
			Status:        types.CostEstimateStatusRateUnavailable,
			EstimateType:  estimateType,
			Basis:         basis,
			PricingRegion: region,
			PricingYear:   pricingYear,
			MissingInputs: []string{"extended_support_rate"},
			Assumptions:   []string{err.Error()},
		}, nil
	}

	hourly := float64(totalVCPU) * rate.PricePerUnit
	monthlyHours := input.MonthlyHours
	if monthlyHours == 0 {
		monthlyHours = 730
	}
	annualHours := input.AnnualHours
	if annualHours == 0 {
		annualHours = 8760
	}

	return &types.CostEstimate{
		CheckID:              c.ID(),
		Status:               types.CostEstimateStatusAvailable,
		EstimateType:         estimateType,
		Basis:                basis,
		Currency:             rate.Currency,
		Unit:                 rate.Unit,
		RatePerUnitHour:      rate.PricePerUnit,
		BillableUnitCount:    float64(totalVCPU),
		HourlyUSD:            hourly,
		MonthlyUSD:           hourly * monthlyHours,
		AnnualUSD:            hourly * annualHours,
		PricingRegion:        region,
		PricingYear:          pricingYear,
		PricingSource:        c.priceSource.SourceName(),
		PricingEffectiveDate: rate.EffectiveDate,
		Confidence:           "estimated",
		Assumptions:          []string{fmt.Sprintf("%.0f hours/month", monthlyHours), fmt.Sprintf("%.0f hours/year", annualHours)},
	}, nil
}

func insufficientEstimate(missing []string) *types.CostEstimate {
	return &types.CostEstimate{
		CheckID:       MySQLExtendedSupportCheckID,
		Status:        types.CostEstimateStatusInsufficientData,
		EstimateType:  estimateType,
		MissingInputs: missing,
	}
}

func estimateBasis(lifecycle *types.LifecycleDetails, asOf time.Time) (types.CostEstimateBasis, bool) {
	if lifecycle.IsExtendedSupport {
		return types.CostEstimateBasisCurrent, true
	}
	if lifecycle.StandardSupportEnd == nil {
		return "", false
	}
	daysUntil := int(lifecycle.StandardSupportEnd.Sub(asOf).Hours() / 24)
	if daysUntil >= 0 && daysUntil <= projectionWindowDays {
		return types.CostEstimateBasisProjected, true
	}
	return "", false
}

func missingJoinInputs(accountID, region, clusterID string) []string {
	var missing []string
	if accountID == "" {
		missing = append(missing, "account_id")
	}
	if region == "" {
		missing = append(missing, "region")
	}
	if clusterID == "" {
		missing = append(missing, "cluster_identifier")
	}
	return missing
}

func clusterJoinParts(finding *types.Finding) (string, string, string) {
	accountID := extraValue(finding.Extra, "account_id", "cloudAccount.externalId", "account", "subscription_external_id")
	region := extraValue(finding.Extra, "region")
	arnAccount, arnRegion, clusterID := parseRDSClusterARN(finding.ResourceID)
	if accountID == "" {
		accountID = arnAccount
	}
	if region == "" {
		region = arnRegion
	}
	return accountID, region, clusterID
}

func extraValue(extra map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := extra[key]; value != "" {
			return value
		}
	}
	return ""
}

func parseRDSClusterARN(raw string) (string, string, string) {
	parts := strings.SplitN(raw, ":", 6)
	if len(parts) != 6 || parts[2] != "rds" {
		return "", "", ""
	}
	resource := parts[5]
	if !strings.HasPrefix(resource, "cluster:") {
		return parts[4], parts[3], ""
	}
	return parts[4], parts[3], strings.TrimPrefix(resource, "cluster:")
}

func isServerlessClass(instanceClass string) bool {
	return strings.Contains(strings.ToLower(instanceClass), "serverless")
}

func normalizeAuroraMySQLMajor(currentVersion, lifecycleVersion string) (string, string) {
	version := strings.ToLower(strings.TrimSpace(currentVersion))
	lifecycleVersion = strings.TrimSpace(lifecycleVersion)

	switch {
	case strings.Contains(version, "mysql_aurora.2"), strings.HasPrefix(version, "5.7"), lifecycleVersion == "2":
		return "2", "5.7"
	case strings.Contains(version, "mysql_aurora.3"), strings.HasPrefix(version, "8.0"), lifecycleVersion == "3":
		return "3", "8.0"
	default:
		return "", ""
	}
}

func extendedSupportPricingYear(standardSupportEnd *time.Time, asOf time.Time) string {
	if standardSupportEnd == nil {
		return pricingYearOneTwo
	}
	if asOf.Before(standardSupportEnd.AddDate(2, 0, 0)) {
		return pricingYearOneTwo
	}
	return pricingYearThree
}
