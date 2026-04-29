package aurora

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	costaws "github.com/block/Version-Guard/pkg/enrichment/cost/aws"
	"github.com/block/Version-Guard/pkg/types"
)

const pricingFixture = `"FormatVersion","v1.0"
"Disclaimer","test"
"Publication Date","2026-04-28T17:07:46Z"
"Version","20260428170746"
"OfferCode","AmazonRDS"
"SKU","OfferTermCode","RateCode","TermType","PriceDescription","EffectiveDate","StartingRange","EndingRange","Unit","PricePerUnit","Currency","RelatedTo","LeaseContractLength","PurchaseOption","OfferingClass","Product Family","serviceCode","Location","Location Type","Instance Type","Current Generation","Instance Family","vCPU","Physical Processor","Clock Speed","Memory","Storage","Network Performance","Processor Architecture","Storage Media","Volume Type","Min Volume Size","Max Volume Size","Engine Code","Database Engine","Database Edition","License Model","Deployment Option","Group","Group Description","usageType","operation","ACU","Dedicated EBS Throughput","Deployment Model","Engine Major Version","Engine Media Type","Enhanced Networking Supported","Extended Support Pricing Year","Instance Type Family","LimitlessPreview","Normalization Size Factor","Processor Features","Region Code","serviceName","Unbundled Licensing","Volume Name","WindowsLicenseMultiplier"
"class-large","JRTCKXETXF","class-large.rate","OnDemand","DB instance","2026-04-01","0","Inf","Hrs","0.29","USD",,,,,,"AmazonRDS","US East (N. Virginia)","AWS Region","db.r6g.large","Yes","Memory optimized","2",,,,,,,,,,,,,,,,,,,,,,,,,,"us-east-1","Amazon Relational Database Service",,,
"class-xlarge","JRTCKXETXF","class-xlarge.rate","OnDemand","DB instance","2026-04-01","0","Inf","Hrs","0.58","USD",,,,,,"AmazonRDS","US East (N. Virginia)","AWS Region","db.r6g.xlarge","Yes","Memory optimized","4",,,,,,,,,,,,,,,,,,,,,,,,,,"us-east-1","Amazon Relational Database Service",,,
"extended","JRTCKXETXF","extended.rate","OnDemand","USD 0.10 per hour per vCPU running RDS Extended Support for Aurora MySQL 2 in Year 1, Year 2","2026-04-01","0","Inf","vCPU-hour","0.1000000000","USD",,,,,,"AmazonRDS","US East (N. Virginia)","AWS Region",,,,,,,,,,,,,,,"16","Aurora MySQL",,,,,,"ExtendedSupport:Yr1-Yr2:AuroraMySQL2","CreateDBInstance:0016",,,,"5.7",,,"Year 1, Year 2",,,,,"us-east-1","Amazon Relational Database Service",,,
`

func TestAuroraMySQLExtendedSupportCheckCalculatesProvisionedSurcharge(t *testing.T) {
	rows, err := costaws.ParseAmazonRDSPriceList(strings.NewReader(pricingFixture))
	require.NoError(t, err)

	instances := NewInstanceIndex([]Instance{
		{
			AccountID:         "123",
			Region:            "us-east-1",
			ClusterIdentifier: "prod",
			DBInstanceClass:   "db.r6g.large",
			Status:            "available",
		},
		{
			AccountID:         "123",
			Region:            "us-east-1",
			ClusterIdentifier: "prod",
			DBInstanceClass:   "db.r6g.xlarge",
			Status:            "available",
		},
	})

	check := NewMySQLExtendedSupportCheck(
		instances,
		costaws.NewStaticRDSPriceSource(rows),
		costaws.NewVCPUResolver(rows),
	)

	standardSupportEnd := time.Date(2024, 10, 31, 0, 0, 0, 0, time.UTC)
	finding := &types.Finding{
		ResourceID:     "arn:aws:rds:us-east-1:123:cluster:prod",
		ResourceType:   "aurora-mysql",
		Engine:         "aurora-mysql",
		CurrentVersion: "5.7.mysql_aurora.2.12.5",
		Extra: map[string]string{
			"account_id": "123",
			"region":     "us-east-1",
		},
		LifecycleDetails: &types.LifecycleDetails{
			Version:            "2",
			StandardSupportEnd: &standardSupportEnd,
			IsExtendedSupport:  true,
		},
	}

	estimate, err := check.Estimate(context.Background(), CostInput{
		Finding:      finding,
		AsOf:         time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC),
		MonthlyHours: 730,
		AnnualHours:  8760,
	})
	require.NoError(t, err)
	require.NotNil(t, estimate)

	assert.Equal(t, "aws.aurora_mysql.extended_support_surcharge", estimate.CheckID)
	assert.Equal(t, types.CostEstimateStatusAvailable, estimate.Status)
	assert.Equal(t, types.CostEstimateBasisCurrent, estimate.Basis)
	assert.Equal(t, "vCPU-hour", estimate.Unit)
	assert.Equal(t, "Year 1, Year 2", estimate.PricingYear)
	assert.Equal(t, "us-east-1", estimate.PricingRegion)
	assert.InDelta(t, 0.10, estimate.RatePerUnitHour, 0.001)
	assert.InDelta(t, 6.0, estimate.BillableUnitCount, 0.001)
	assert.InDelta(t, 0.60, estimate.HourlyUSD, 0.001)
	assert.InDelta(t, 438.00, estimate.MonthlyUSD, 0.001)
	assert.InDelta(t, 5256.00, estimate.AnnualUSD, 0.001)
}

func TestAuroraMySQLExtendedSupportCheckCalculatesProjectedSurcharge(t *testing.T) {
	rows, err := costaws.ParseAmazonRDSPriceList(strings.NewReader(pricingFixture))
	require.NoError(t, err)

	check := NewMySQLExtendedSupportCheck(
		NewInstanceIndex([]Instance{
			{
				AccountID:         "123",
				Region:            "us-east-1",
				ClusterIdentifier: "prod",
				DBInstanceClass:   "db.r6g.large",
				Status:            "available",
			},
		}),
		costaws.NewStaticRDSPriceSource(rows),
		costaws.NewVCPUResolver(rows),
	)

	standardSupportEnd := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	finding := auroraMySQLFinding(&standardSupportEnd)
	finding.LifecycleDetails.IsExtendedSupport = false
	estimate, err := check.Estimate(context.Background(), CostInput{
		Finding:      finding,
		AsOf:         time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC),
		MonthlyHours: 730,
		AnnualHours:  8760,
	})
	require.NoError(t, err)
	require.NotNil(t, estimate)

	assert.Equal(t, types.CostEstimateBasisProjected, estimate.Basis)
	assert.InDelta(t, 146.00, estimate.MonthlyUSD, 0.001)
}

func TestAuroraMySQLExtendedSupportCheckReportsMissingInstanceRows(t *testing.T) {
	rows, err := costaws.ParseAmazonRDSPriceList(strings.NewReader(pricingFixture))
	require.NoError(t, err)

	check := NewMySQLExtendedSupportCheck(
		NewInstanceIndex(nil),
		costaws.NewStaticRDSPriceSource(rows),
		costaws.NewVCPUResolver(rows),
	)

	standardSupportEnd := time.Date(2024, 10, 31, 0, 0, 0, 0, time.UTC)
	estimate, err := check.Estimate(context.Background(), CostInput{
		Finding: &types.Finding{
			ResourceID:     "arn:aws:rds:us-east-1:123:cluster:prod",
			ResourceType:   "aurora-mysql",
			Engine:         "aurora-mysql",
			CurrentVersion: "5.7",
			Extra: map[string]string{
				"account_id": "123",
				"region":     "us-east-1",
			},
			LifecycleDetails: &types.LifecycleDetails{
				StandardSupportEnd: &standardSupportEnd,
				IsExtendedSupport:  true,
			},
		},
		AsOf:         time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC),
		MonthlyHours: 730,
		AnnualHours:  8760,
	})
	require.NoError(t, err)
	require.NotNil(t, estimate)

	assert.Equal(t, types.CostEstimateStatusInsufficientData, estimate.Status)
	assert.Contains(t, estimate.MissingInputs, "aurora_instance_rows")
}

func TestAuroraMySQLExtendedSupportCheckReportsMissingClass(t *testing.T) {
	estimate := estimateWithInstances(t, []Instance{
		{
			AccountID:         "123",
			Region:            "us-east-1",
			ClusterIdentifier: "prod",
			Status:            "available",
		},
	}, nil)

	assert.Equal(t, types.CostEstimateStatusInsufficientData, estimate.Status)
	assert.Contains(t, estimate.MissingInputs, "db_instance_class")
}

func TestAuroraMySQLExtendedSupportCheckReportsUnknownClass(t *testing.T) {
	estimate := estimateWithInstances(t, []Instance{
		{
			AccountID:         "123",
			Region:            "us-east-1",
			ClusterIdentifier: "prod",
			DBInstanceClass:   "db.unknown.large",
			Status:            "available",
		},
	}, nil)

	assert.Equal(t, types.CostEstimateStatusInsufficientData, estimate.Status)
	assert.Contains(t, estimate.MissingInputs, "vcpu_count")
}

func TestAuroraMySQLExtendedSupportCheckReportsServerlessWithoutACUUsage(t *testing.T) {
	estimate := estimateWithInstances(t, []Instance{
		{
			AccountID:         "123",
			Region:            "us-east-1",
			ClusterIdentifier: "prod",
			DBInstanceClass:   "db.serverless",
			Status:            "available",
		},
	}, nil)

	assert.Equal(t, types.CostEstimateStatusInsufficientData, estimate.Status)
	assert.Contains(t, estimate.MissingInputs, "serverless_acu_usage")
}

func TestAuroraMySQLExtendedSupportCheckReportsMissingRate(t *testing.T) {
	estimate := estimateWithInstances(t, []Instance{
		{
			AccountID:         "123",
			Region:            "us-east-1",
			ClusterIdentifier: "prod",
			DBInstanceClass:   "db.r6g.large",
			Status:            "available",
		},
	}, []costaws.PriceListRow{
		{InstanceType: "db.r6g.large", VCPU: 2, RegionCode: "us-east-1"},
	})

	assert.Equal(t, types.CostEstimateStatusRateUnavailable, estimate.Status)
	assert.Contains(t, estimate.MissingInputs, "extended_support_rate")
}

func TestNormalizeAuroraMySQLMajor(t *testing.T) {
	tests := []struct {
		name             string
		currentVersion   string
		lifecycleVersion string
		wantAuroraMajor  string
		wantEngineMajor  string
	}{
		{name: "aurora v2 marker", currentVersion: "5.7.mysql_aurora.2.12.5", wantAuroraMajor: "2", wantEngineMajor: "5.7"},
		{name: "aurora v3 marker", currentVersion: "8.0.mysql_aurora.3.05.2", wantAuroraMajor: "3", wantEngineMajor: "8.0"},
		{name: "lifecycle v2", lifecycleVersion: "2", wantAuroraMajor: "2", wantEngineMajor: "5.7"},
		{name: "lifecycle v3", lifecycleVersion: "3", wantAuroraMajor: "3", wantEngineMajor: "8.0"},
		{name: "unknown", currentVersion: "5.6.10a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAuroraMajor, gotEngineMajor := normalizeAuroraMySQLMajor(tt.currentVersion, tt.lifecycleVersion)
			assert.Equal(t, tt.wantAuroraMajor, gotAuroraMajor)
			assert.Equal(t, tt.wantEngineMajor, gotEngineMajor)
		})
	}
}

func TestParseInstanceReportAcceptsCommonWizColumns(t *testing.T) {
	report := `externalId,nativeType,cloudAccount.externalId,region,typeFields.dbClusterIdentifier,typeFields.dbInstanceClass,status,typeFields.kind,versionDetails.version
arn:aws:rds:us-east-1:123:db:prod-1,rds/AmazonAuroraMySQL/instance,123,us-east-1,prod,db.r6g.large,available,AmazonAuroraMySQL,5.7.mysql_aurora.2.12.5
arn:aws:rds:us-east-1:123:cluster:prod,rds/AmazonAuroraMySQL/cluster,123,us-east-1,prod,,available,AmazonAuroraMySQL,5.7.mysql_aurora.2.12.5
`

	instances, err := ParseInstanceReport(strings.NewReader(report))
	require.NoError(t, err)

	require.Len(t, instances, 1)
	assert.Equal(t, "arn:aws:rds:us-east-1:123:db:prod-1", instances[0].ResourceID)
	assert.Equal(t, "123", instances[0].AccountID)
	assert.Equal(t, "us-east-1", instances[0].Region)
	assert.Equal(t, "prod", instances[0].ClusterIdentifier)
	assert.Equal(t, "db.r6g.large", instances[0].DBInstanceClass)
}

func estimateWithInstances(t *testing.T, instances []Instance, rows []costaws.PriceListRow) *types.CostEstimate {
	t.Helper()
	if rows == nil {
		parsed, err := costaws.ParseAmazonRDSPriceList(strings.NewReader(pricingFixture))
		require.NoError(t, err)
		rows = parsed
	}
	standardSupportEnd := time.Date(2024, 10, 31, 0, 0, 0, 0, time.UTC)
	check := NewMySQLExtendedSupportCheck(
		NewInstanceIndex(instances),
		costaws.NewStaticRDSPriceSource(rows),
		costaws.NewVCPUResolver(rows),
	)
	estimate, err := check.Estimate(context.Background(), CostInput{
		Finding:      auroraMySQLFinding(&standardSupportEnd),
		AsOf:         time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC),
		MonthlyHours: 730,
		AnnualHours:  8760,
	})
	require.NoError(t, err)
	require.NotNil(t, estimate)
	return estimate
}

func auroraMySQLFinding(standardSupportEnd *time.Time) *types.Finding {
	return &types.Finding{
		ResourceID:     "arn:aws:rds:us-east-1:123:cluster:prod",
		ResourceType:   "aurora-mysql",
		Engine:         "aurora-mysql",
		CurrentVersion: "5.7.mysql_aurora.2.12.5",
		Extra: map[string]string{
			"account_id": "123",
			"region":     "us-east-1",
		},
		LifecycleDetails: &types.LifecycleDetails{
			Version:            "2",
			StandardSupportEnd: standardSupportEnd,
			IsExtendedSupport:  true,
		},
	}
}
