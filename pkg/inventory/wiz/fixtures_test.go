package wiz

import (
	"io"
	"strings"
	"time"
)

// WizAPIFixtures contains realistic Wiz API response data for testing
var WizAPIFixtures = struct {
	AccessToken        string
	AuroraReport       *Report
	AuroraCSVData      string
	ElastiCacheReport  *Report
	ElastiCacheCSVData string
	LambdaReport       *Report
	LambdaCSVData      string
	EmptyCSVData       string
	MalformedCSVData   string
}{
	AccessToken: "wiz-mock-access-token-12345",

	AuroraReport: &Report{
		ID:               "aurora-report-id-123",
		Name:             "Aurora Clusters Report",
		DownloadURL:      "https://wiz-api.example.com/reports/aurora-report-id-123/download",
		LastRun:          time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC),
		RunIntervalHours: 24,
		ExpectedRows:     5,
	},

	// Realistic Wiz CSV export for Aurora clusters
	// Columns: externalId, name, nativeType, cloudAccount.externalId, versionDetails.version, region, tags, typeFields.kind
	AuroraCSVData: `externalId,name,nativeType,cloudAccount.externalId,versionDetails.version,region,tags,typeFields.kind
arn:aws:rds:us-east-1:123456789012:cluster:legacy-mysql-56,legacy-mysql-56,rds/AmazonAuroraMySQL/cluster,123456789012,5.6.10a,us-east-1,"[{""key"":""app"",""value"":""legacy-payments""},{""key"":""environment"",""value"":""production""},{""key"":""brand"",""value"":""brand-a""}]",AmazonAuroraMySQL
arn:aws:rds:us-east-1:123456789012:cluster:mysql-57-extended,mysql-57-extended,rds/AmazonAuroraMySQL/cluster,123456789012,5.7.12,us-east-1,"[{""key"":""application"",""value"":""billing""},{""key"":""env"",""value"":""production""}]",AmazonAuroraMySQL
arn:aws:rds:us-east-1:123456789012:cluster:mysql-57-approaching,mysql-57-approaching,rds/AmazonAuroraMySQL/cluster,123456789012,5.7.44,us-east-1,"[{""key"":""app"",""value"":""analytics""},{""key"":""env"",""value"":""production""},{""key"":""brand"",""value"":""brand-b""}]",AmazonAuroraMySQL
arn:aws:rds:us-west-2:789012345678:cluster:mysql-80-current,mysql-80-current,rds/AmazonAuroraMySQL/cluster,789012345678,8.0.mysql_aurora.3.05.2,us-west-2,"[{""key"":""app"",""value"":""payments""},{""key"":""team"",""value"":""team-a""}]",AmazonAuroraMySQL
arn:aws:rds:eu-west-1:345678901234:cluster:postgres-11-deprecated,postgres-11-deprecated,rds/AmazonAuroraPostgreSQL/cluster,345678901234,11.21,eu-west-1,"[{""key"":""app"",""value"":""user-service""},{""key"":""brand"",""value"":""brand-c""}]",AmazonAuroraPostgreSQL
`,

	ElastiCacheReport: &Report{
		ID:               "elasticache-report-id-456",
		Name:             "ElastiCache Version Report",
		DownloadURL:      "https://wiz-api.example.com/reports/elasticache-report-id-456/download",
		LastRun:          time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC),
		RunIntervalHours: 24,
		ExpectedRows:     5,
	},

	// Realistic Wiz CSV export for ElastiCache clusters
	// Columns: externalId, name, nativeType, cloudAccount.externalId, versionDetails.version, region, tags, typeFields.kind
	ElastiCacheCSVData: `externalId,name,nativeType,cloudAccount.externalId,versionDetails.version,region,tags,typeFields.kind
arn:aws:elasticache:us-east-1:123456789012:cluster:legacy-redis-001,legacy-redis-001,elastiCache/Redis/cluster,123456789012,6.2.6,us-east-1,"[{""key"":""app"",""value"":""legacy-payments""},{""key"":""environment"",""value"":""production""},{""key"":""brand"",""value"":""brand-a""}]",Redis
arn:aws:elasticache:us-east-1:123456789012:cluster:billing-redis-001,billing-redis-001,elastiCache/Redis/cluster,123456789012,7.0.7,us-east-1,"[{""key"":""application"",""value"":""billing""},{""key"":""env"",""value"":""production""}]",Redis
arn:aws:elasticache:us-east-1:123456789012:cluster:analytics-redis-001,analytics-redis-001,elastiCache/Redis/cluster,123456789012,7.1.0,us-east-1,"[{""key"":""app"",""value"":""analytics""},{""key"":""env"",""value"":""production""},{""key"":""brand"",""value"":""brand-b""}]",Redis
arn:aws:elasticache:us-west-2:789012345678:cluster:session-memcached-001,session-memcached-001,elastiCache/Memcached/cluster,789012345678,,us-west-2,"[{""key"":""app"",""value"":""session-store""},{""key"":""team"",""value"":""team-a""}]",Memcached
arn:aws:elasticache:eu-west-1:345678901234:cluster:user-valkey-001,user-valkey-001,elastiCache/Valkey/cluster,345678901234,,eu-west-1,"[{""key"":""app"",""value"":""user-service""},{""key"":""brand"",""value"":""brand-c""}]",Valkey
`,

	LambdaReport: &Report{
		ID:               "lambda-report-id-789",
		Name:             "Lambda Functions Report",
		DownloadURL:      "https://wiz-api.example.com/reports/lambda-report-id-789/download",
		LastRun:          time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
		RunIntervalHours: 24,
		ExpectedRows:     5,
	},

	// Realistic Wiz CSV export for Lambda functions
	// nativeType is "lambda" for all functions (verified against production Wiz data).
	// Columns: externalId, name, nativeType, cloudAccount.externalId, region, tags, graphEntity.properties
	LambdaCSVData: `externalId,name,nativeType,cloudAccount.externalId,region,tags,graphEntity.properties
arn:aws:lambda:us-east-1:123456789012:function:legacy-python38,legacy-python38,lambda,123456789012,us-east-1,"[{""key"":""app"",""value"":""legacy-payments""},{""key"":""environment"",""value"":""production""},{""key"":""brand"",""value"":""brand-a""}]","{""runtime"":""python3.8"",""memorySize"":256,""timeout"":30}"
arn:aws:lambda:us-east-1:123456789012:function:billing-node20,billing-node20,lambda,123456789012,us-east-1,"[{""key"":""application"",""value"":""billing""},{""key"":""env"",""value"":""production""}]","{""runtime"":""nodejs20.x"",""memorySize"":512,""timeout"":60}"
arn:aws:lambda:us-west-2:789012345678:function:payments-java21,payments-java21,lambda,789012345678,us-west-2,"[{""key"":""app"",""value"":""payments""},{""key"":""team"",""value"":""team-a""}]","{""runtime"":""java21"",""memorySize"":1024,""timeout"":120}"
arn:aws:lambda:eu-west-1:345678901234:function:custom-runtime,custom-runtime,lambda,345678901234,eu-west-1,"[{""key"":""app"",""value"":""user-service""},{""key"":""brand"",""value"":""brand-c""}]","{""runtime"":""provided.al2023"",""memorySize"":128,""timeout"":15}"
arn:aws:lambda:us-east-1:123456789012:function:no-runtime-props,no-runtime-props,lambda,123456789012,us-east-1,"[{""key"":""app"",""value"":""analytics""}]","{""memorySize"":256}"
`,

	EmptyCSVData: `externalId,name,nativeType,cloudAccount.externalId,versionDetails.version,region,tags,typeFields.kind
`,

	MalformedCSVData: `ID,Name,CloudProvider
malformed-row-with-missing-columns
`,
}

// NewMockReadCloser creates an io.ReadCloser from a string (for mocking CSV downloads)
func NewMockReadCloser(data string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(data))
}
