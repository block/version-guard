package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	cost "github.com/block/Version-Guard/pkg/enrichment/cost"
	costaws "github.com/block/Version-Guard/pkg/enrichment/cost/aws"
	"github.com/block/Version-Guard/pkg/enrichment/cost/aws/aurora"
	"github.com/block/Version-Guard/pkg/inventory/wiz"
	"github.com/block/Version-Guard/pkg/types"
)

var version = "dev"

type CLI struct {
	Cost CostCmd `cmd:"" help:"Run cost enrichment checks"`
}

type s3ObjectAPI interface {
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

var newS3ObjectClient = func(ctx context.Context) (s3ObjectAPI, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(cfg), nil
}

// CostCmd enriches a snapshot with registered cost checks.
//
//nolint:govet // field alignment sacrificed for flag readability
type CostCmd struct {
	InputFile                        string    `help:"Input snapshot JSON file"`
	OutputFile                       string    `help:"Output enriched snapshot JSON file"`
	InputS3URI                       string    `name:"input-s3-uri" help:"Input snapshot S3 URI, e.g. s3://bucket/key"`
	OutputS3URI                      string    `name:"output-s3-uri" help:"Output enriched snapshot S3 URI, e.g. s3://bucket/key"`
	WizAuroraMySQLInstanceReportFile string    `name:"wiz-aurora-mysql-instance-report-file" help:"Local Wiz Aurora MySQL instance CSV report"`
	WizAuroraMySQLInstanceReportID   string    `name:"wiz-aurora-mysql-instance-report-id" help:"Wiz saved report ID for Aurora MySQL instance inventory" env:"WIZ_AURORA_MYSQL_INSTANCE_REPORT_ID"`
	WizClientIDSecret                string    `help:"Wiz client ID" env:"WIZ_CLIENT_ID_SECRET"`
	WizClientSecretSecret            string    `help:"Wiz client secret" env:"WIZ_CLIENT_SECRET_SECRET"`
	PricingFile                      string    `help:"Local AWS AmazonRDS price list CSV"`
	MonthlyHours                     float64   `help:"Monthly hours used for estimates" default:"730"`
	AnnualHours                      float64   `help:"Annual hours used for estimates" default:"8760"`
	AsOf                             time.Time `help:"As-of time for pricing-year decisions" default:"${default_as_of}"`
}

func (c CostCmd) Run() error {
	return runCost(context.Background(), c)
}

func runCost(ctx context.Context, cmd CostCmd) error {
	if cmd.AsOf.IsZero() {
		cmd.AsOf = time.Now().UTC()
	}
	snapshot, err := readSnapshot(ctx, cmd)
	if err != nil {
		return err
	}

	instances, err := readAuroraInstances(ctx, cmd)
	if err != nil {
		return err
	}

	priceRows, err := readPriceRows(ctx, cmd, snapshot)
	if err != nil {
		return err
	}

	runner := cost.NewRunner(
		cost.WithAsOf(cmd.AsOf),
		cost.WithHours(cmd.MonthlyHours, cmd.AnnualHours),
		cost.WithChecks(aurora.NewMySQLExtendedSupportCheck(
			aurora.NewInstanceIndex(instances),
			costaws.NewStaticRDSPriceSource(priceRows),
			costaws.NewVCPUResolver(priceRows),
		)),
	)

	enriched, err := runner.Enrich(ctx, snapshot)
	if err != nil {
		return fmt.Errorf("run cost enrichment: %w", err)
	}

	return writeSnapshot(ctx, cmd, enriched)
}

func readSnapshot(ctx context.Context, cmd CostCmd) (*types.Snapshot, error) {
	raw, err := readInput(ctx, cmd.InputFile, cmd.InputS3URI)
	if err != nil {
		return nil, fmt.Errorf("read input snapshot: %w", err)
	}
	var snapshot types.Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("decode input snapshot: %w", err)
	}
	return &snapshot, nil
}

func writeSnapshot(ctx context.Context, cmd CostCmd, snapshot *types.Snapshot) error {
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode enriched snapshot: %w", err)
	}
	if err := writeOutput(ctx, cmd.OutputFile, cmd.OutputS3URI, raw); err != nil {
		return fmt.Errorf("write enriched snapshot: %w", err)
	}
	return nil
}

func readInput(ctx context.Context, filePath, s3URI string) ([]byte, error) {
	switch {
	case filePath != "":
		return os.ReadFile(filePath)
	case s3URI != "":
		bucket, key, err := parseS3URI(s3URI)
		if err != nil {
			return nil, err
		}
		client, err := newS3ObjectClient(ctx)
		if err != nil {
			return nil, err
		}
		result, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return nil, err
		}
		defer result.Body.Close()
		return io.ReadAll(result.Body)
	default:
		return nil, fmt.Errorf("either --input-file or --input-s3-uri is required")
	}
}

func writeOutput(ctx context.Context, filePath, s3URI string, raw []byte) error {
	switch {
	case filePath != "":
		return os.WriteFile(filePath, raw, 0o600)
	case s3URI != "":
		bucket, key, err := parseS3URI(s3URI)
		if err != nil {
			return err
		}
		client, err := newS3ObjectClient(ctx)
		if err != nil {
			return err
		}
		_, err = client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(key),
			Body:        bytes.NewReader(raw),
			ContentType: aws.String("application/json"),
		})
		return err
	default:
		return fmt.Errorf("either --output-file or --output-s3-uri is required")
	}
}

func parseS3URI(uri string) (string, string, error) {
	if !strings.HasPrefix(uri, "s3://") {
		return "", "", fmt.Errorf("invalid S3 URI %q", uri)
	}
	trimmed := strings.TrimPrefix(uri, "s3://")
	bucket, key, ok := strings.Cut(trimmed, "/")
	if !ok || bucket == "" || key == "" {
		return "", "", fmt.Errorf("invalid S3 URI %q", uri)
	}
	return bucket, key, nil
}

func readAuroraInstances(ctx context.Context, cmd CostCmd) ([]aurora.Instance, error) {
	switch {
	case cmd.WizAuroraMySQLInstanceReportFile != "":
		f, err := os.Open(cmd.WizAuroraMySQLInstanceReportFile)
		if err != nil {
			return nil, fmt.Errorf("open Aurora instance report: %w", err)
		}
		defer f.Close()
		return aurora.ParseInstanceReport(f)
	case cmd.WizAuroraMySQLInstanceReportID != "":
		if cmd.WizClientIDSecret == "" || cmd.WizClientSecretSecret == "" {
			return nil, fmt.Errorf("Wiz credentials are required when using --wiz-aurora-mysql-instance-report-id")
		}
		client := wiz.NewClient(
			wiz.NewHTTPClient(cmd.WizClientIDSecret, cmd.WizClientSecretSecret),
			wiz.DefaultCacheTTL,
		)
		rows, err := client.GetReportData(ctx, cmd.WizAuroraMySQLInstanceReportID)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		writer := csv.NewWriter(&buf)
		if err := writer.WriteAll(rows); err != nil {
			return nil, err
		}
		return aurora.ParseInstanceReport(&buf)
	default:
		return nil, fmt.Errorf("either --wiz-aurora-mysql-instance-report-file or --wiz-aurora-mysql-instance-report-id is required")
	}
}

func readPriceRows(ctx context.Context, cmd CostCmd, snapshot *types.Snapshot) ([]costaws.PriceListRow, error) {
	if cmd.PricingFile != "" {
		f, err := os.Open(cmd.PricingFile)
		if err != nil {
			return nil, fmt.Errorf("open pricing file: %w", err)
		}
		defer f.Close()
		return costaws.ParseAmazonRDSPriceList(f)
	}

	var allRows []costaws.PriceListRow
	for _, region := range auroraMySQLRegions(snapshot) {
		rows, err := costaws.FetchAmazonRDSPriceList(ctx, region)
		if err != nil {
			return nil, err
		}
		allRows = append(allRows, rows...)
	}
	if len(allRows) == 0 {
		return nil, fmt.Errorf("no Aurora MySQL regions found in snapshot and no --pricing-file provided")
	}
	return allRows, nil
}

func auroraMySQLRegions(snapshot *types.Snapshot) []string {
	seen := make(map[string]bool)
	var regions []string
	if snapshot == nil {
		return regions
	}
	for _, findings := range snapshot.FindingsByType {
		for _, finding := range findings {
			region := findingRegion(finding)
			if finding.Engine != "aurora-mysql" || region == "" {
				continue
			}
			if !seen[region] {
				seen[region] = true
				regions = append(regions, region)
			}
		}
	}
	return regions
}

func findingRegion(finding *types.Finding) string {
	if finding == nil {
		return ""
	}
	if region := finding.Extra["region"]; region != "" {
		return region
	}
	parts := strings.SplitN(finding.ResourceID, ":", 6)
	if len(parts) == 6 && parts[2] == "rds" {
		return parts[3]
	}
	return ""
}

func main() {
	cli := &CLI{}
	ctx := kong.Parse(cli,
		kong.Name("version-guard-enricher"),
		kong.Description("Version Guard enrichment utilities"),
		kong.UsageOnError(),
		kong.Vars{
			"version":       version,
			"default_as_of": time.Now().UTC().Format(time.RFC3339),
		},
	)
	ctx.FatalIfErrorf(ctx.Run())
}
