package aws

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const priceListSourceName = "aws-price-list-amazon-rds"

// PriceListRow contains the AmazonRDS price list fields needed for enrichment.
//
//nolint:govet // field alignment sacrificed for readability
type PriceListRow struct {
	EffectiveDate              *time.Time
	SKU                        string
	RateCode                   string
	PriceDescription           string
	Unit                       string
	Currency                   string
	DatabaseEngine             string
	UsageType                  string
	Operation                  string
	InstanceType               string
	EngineMajorVersion         string
	ExtendedSupportPricingYear string
	RegionCode                 string
	ServiceName                string
	PricePerUnit               float64
	VCPU                       int
}

// RateQuery identifies one Extended Support pricing row.
type RateQuery struct {
	RegionCode                 string
	DatabaseEngine             string
	EngineMajorVersion         string
	Unit                       string
	ExtendedSupportPricingYear string
	UsageTypeContains          string
}

// StaticRDSPriceSource resolves rates from parsed AmazonRDS price list rows.
type StaticRDSPriceSource struct {
	rows []PriceListRow
}

// NewStaticRDSPriceSource creates a static price source from parsed rows.
func NewStaticRDSPriceSource(rows []PriceListRow) *StaticRDSPriceSource {
	return &StaticRDSPriceSource{rows: rows}
}

// FindRate resolves the best matching Extended Support row.
func (s *StaticRDSPriceSource) FindRate(_ context.Context, query RateQuery) (*PriceListRow, error) {
	for _, row := range s.rows {
		if query.RegionCode != "" && row.RegionCode != query.RegionCode {
			continue
		}
		if query.DatabaseEngine != "" && !strings.EqualFold(row.DatabaseEngine, query.DatabaseEngine) {
			continue
		}
		if query.EngineMajorVersion != "" && row.EngineMajorVersion != query.EngineMajorVersion {
			continue
		}
		if query.Unit != "" && row.Unit != query.Unit {
			continue
		}
		if query.ExtendedSupportPricingYear != "" && row.ExtendedSupportPricingYear != query.ExtendedSupportPricingYear {
			continue
		}
		if query.UsageTypeContains != "" && !strings.Contains(row.UsageType, query.UsageTypeContains) {
			continue
		}
		if !strings.Contains(row.UsageType, "ExtendedSupport") && !strings.Contains(row.PriceDescription, "Extended Support") {
			continue
		}
		return &row, nil
	}
	return nil, fmt.Errorf("no AmazonRDS Extended Support rate found for %+v", query)
}

// SourceName returns a stable name for metadata.
func (s *StaticRDSPriceSource) SourceName() string {
	return priceListSourceName
}

// ParseAmazonRDSPriceList parses an AWS AmazonRDS offer CSV.
func ParseAmazonRDSPriceList(r io.Reader) ([]PriceListRow, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1

	var header []string
	for {
		row, err := reader.Read()
		if err != nil {
			return nil, fmt.Errorf("read price list header: %w", err)
		}
		if len(row) > 0 && row[0] == "SKU" {
			header = row
			break
		}
	}

	cols := make(map[string]int, len(header))
	for i, name := range header {
		cols[name] = i
	}

	var rows []PriceListRow
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read price list row: %w", err)
		}
		rows = append(rows, parsePriceListRow(cols, row))
	}
	return rows, nil
}

func parsePriceListRow(cols map[string]int, row []string) PriceListRow {
	effectiveDate := parsePriceListTime(col(cols, row, "EffectiveDate"))
	return PriceListRow{
		EffectiveDate:              effectiveDate,
		SKU:                        col(cols, row, "SKU"),
		RateCode:                   col(cols, row, "RateCode"),
		PriceDescription:           col(cols, row, "PriceDescription"),
		Unit:                       col(cols, row, "Unit"),
		PricePerUnit:               parseFloat(col(cols, row, "PricePerUnit")),
		Currency:                   col(cols, row, "Currency"),
		DatabaseEngine:             col(cols, row, "Database Engine"),
		UsageType:                  col(cols, row, "usageType"),
		Operation:                  col(cols, row, "operation"),
		InstanceType:               col(cols, row, "Instance Type"),
		EngineMajorVersion:         col(cols, row, "Engine Major Version"),
		ExtendedSupportPricingYear: col(cols, row, "Extended Support Pricing Year"),
		RegionCode:                 col(cols, row, "Region Code"),
		ServiceName:                col(cols, row, "serviceName"),
		VCPU:                       parseInt(col(cols, row, "vCPU")),
	}
}

func col(cols map[string]int, row []string, name string) string {
	idx, ok := cols[name]
	if !ok || idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

func parseFloat(raw string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return v
}

func parseInt(raw string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(raw))
	return v
}

func parsePriceListTime(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

// FetchAmazonRDSPriceList downloads and parses the public AmazonRDS price list for a region.
func FetchAmazonRDSPriceList(ctx context.Context, region string) ([]PriceListRow, error) {
	url := fmt.Sprintf("https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonRDS/current/%s/index.csv", region)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch AmazonRDS price list: status %d", resp.StatusCode)
	}
	return ParseAmazonRDSPriceList(resp.Body)
}
