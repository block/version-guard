package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/Version-Guard/pkg/types"
)

const cliPricingFixture = `"FormatVersion","v1.0"
"Disclaimer","test"
"Publication Date","2026-04-28T17:07:46Z"
"Version","20260428170746"
"OfferCode","AmazonRDS"
"SKU","OfferTermCode","RateCode","TermType","PriceDescription","EffectiveDate","StartingRange","EndingRange","Unit","PricePerUnit","Currency","RelatedTo","LeaseContractLength","PurchaseOption","OfferingClass","Product Family","serviceCode","Location","Location Type","Instance Type","Current Generation","Instance Family","vCPU","Physical Processor","Clock Speed","Memory","Storage","Network Performance","Processor Architecture","Storage Media","Volume Type","Min Volume Size","Max Volume Size","Engine Code","Database Engine","Database Edition","License Model","Deployment Option","Group","Group Description","usageType","operation","ACU","Dedicated EBS Throughput","Deployment Model","Engine Major Version","Engine Media Type","Enhanced Networking Supported","Extended Support Pricing Year","Instance Type Family","LimitlessPreview","Normalization Size Factor","Processor Features","Region Code","serviceName","Unbundled Licensing","Volume Name","WindowsLicenseMultiplier"
"class-large","JRTCKXETXF","class-large.rate","OnDemand","DB instance","2026-04-01","0","Inf","Hrs","0.29","USD",,,,,,"AmazonRDS","US East (N. Virginia)","AWS Region","db.r6g.large","Yes","Memory optimized","2",,,,,,,,,,,,,,,,,,,,,,,,,,"us-east-1","Amazon Relational Database Service",,,
"extended","JRTCKXETXF","extended.rate","OnDemand","USD 0.10 per hour per vCPU running RDS Extended Support for Aurora MySQL 2 in Year 1, Year 2","2026-04-01","0","Inf","vCPU-hour","0.1000000000","USD",,,,,,"AmazonRDS","US East (N. Virginia)","AWS Region",,,,,,,,,,,,,,,"16","Aurora MySQL",,,,,,"ExtendedSupport:Yr1-Yr2:AuroraMySQL2","CreateDBInstance:0016",,,,"5.7",,,"Year 1, Year 2",,,,,"us-east-1","Amazon Relational Database Service",,,
`

func TestRunCostLocalFilesWritesEnrichedSnapshot(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "snapshot.json")
	outputFile := filepath.Join(dir, "snapshot.enriched.json")
	instanceFile := filepath.Join(dir, "aurora-instances.csv")
	pricingFile := filepath.Join(dir, "pricing.csv")

	standardSupportEnd := time.Date(2024, 10, 31, 0, 0, 0, 0, time.UTC)
	base := &types.Snapshot{
		SnapshotID: "snapshot-1",
		Version:    "v1",
		FindingsByType: map[types.ResourceType][]*types.Finding{
			"aurora-mysql": {
				{
					ResourceID:     "arn:aws:rds:us-east-1:123:cluster:prod",
					ResourceType:   "aurora-mysql",
					Engine:         "aurora-mysql",
					CurrentVersion: "5.7.mysql_aurora.2.12.5",
					Service:        "payments",
					Extra: map[string]string{
						"account_id": "123",
						"region":     "us-east-1",
					},
					LifecycleDetails: &types.LifecycleDetails{
						Version:            "2",
						StandardSupportEnd: &standardSupportEnd,
						IsExtendedSupport:  true,
					},
				},
			},
		},
	}

	raw, err := json.Marshal(base)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(inputFile, raw, 0o600))
	require.NoError(t, os.WriteFile(instanceFile, []byte(`externalId,nativeType,cloudAccount.externalId,region,typeFields.dbClusterIdentifier,typeFields.dbInstanceClass,status,typeFields.kind,versionDetails.version
arn:aws:rds:us-east-1:123:db:prod-1,rds/AmazonAuroraMySQL/instance,123,us-east-1,prod,db.r6g.large,available,AmazonAuroraMySQL,5.7.mysql_aurora.2.12.5
`), 0o600))
	require.NoError(t, os.WriteFile(pricingFile, []byte(cliPricingFixture), 0o600))

	err = runCost(context.Background(), CostCmd{
		InputFile:                        inputFile,
		OutputFile:                       outputFile,
		WizAuroraMySQLInstanceReportFile: instanceFile,
		PricingFile:                      pricingFile,
		MonthlyHours:                     730,
		AnnualHours:                      8760,
		AsOf:                             time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	enrichedRaw, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	var enriched types.Snapshot
	require.NoError(t, json.Unmarshal(enrichedRaw, &enriched))

	findings := enriched.FindingsByType["aurora-mysql"]
	require.Len(t, findings, 1)
	require.Len(t, findings[0].CostEstimates, 1)
	assert.Equal(t, types.CostEstimateStatusAvailable, findings[0].CostEstimates[0].Status)
	assert.InDelta(t, 146.00, findings[0].CostEstimates[0].MonthlyUSD, 0.001)
	require.NotNil(t, enriched.Summary.CostSummary)
	assert.InDelta(t, 146.00, enriched.Summary.CostSummary.CurrentMonthlyUSD, 0.001)
	assert.Equal(t, 1, enriched.Summary.CostSummary.ByService["payments"].AvailableCount)
}

func TestReadWriteS3UsesURIClient(t *testing.T) {
	oldClient := newS3ObjectClient
	defer func() { newS3ObjectClient = oldClient }()

	fake := &fakeS3ObjectClient{
		objects: map[string][]byte{
			"bucket/input.json": []byte(`{"snapshot_id":"snapshot-1"}`),
		},
	}
	newS3ObjectClient = func(context.Context) (s3ObjectAPI, error) {
		return fake, nil
	}

	raw, err := readInput(context.Background(), "", "s3://bucket/input.json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"snapshot_id":"snapshot-1"}`, string(raw))

	err = writeOutput(context.Background(), "", "s3://bucket/output.json", []byte(`{"ok":true}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, string(fake.objects["bucket/output.json"]))
}

type fakeS3ObjectClient struct {
	objects map[string][]byte
}

func (f *fakeS3ObjectClient) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	key := s3ObjectKey(in.Bucket, in.Key)
	raw, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("object %q not found", key)
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(raw))}, nil
}

func (f *fakeS3ObjectClient) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	raw, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	if f.objects == nil {
		f.objects = make(map[string][]byte)
	}
	f.objects[s3ObjectKey(in.Bucket, in.Key)] = raw
	return &s3.PutObjectOutput{}, nil
}

func s3ObjectKey(bucket, key *string) string {
	return *bucket + "/" + *key
}
