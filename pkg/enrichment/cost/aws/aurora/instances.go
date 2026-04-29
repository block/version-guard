package aurora

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

const auroraMySQLInstanceNativeType = "rds/AmazonAuroraMySQL/instance"

// Instance is the Aurora instance metadata needed for surcharge estimates.
type Instance struct {
	ResourceID        string
	AccountID         string
	Region            string
	ClusterIdentifier string
	DBInstanceClass   string
	Status            string
	Engine            string
	EngineVersion     string
}

// InstanceIndex groups Aurora instances by cluster join key.
type InstanceIndex struct {
	byCluster map[string][]Instance
}

// NewInstanceIndex indexes instances by account, region, and cluster identifier.
func NewInstanceIndex(instances []Instance) *InstanceIndex {
	idx := &InstanceIndex{byCluster: make(map[string][]Instance)}
	for _, instance := range instances {
		if instance.AccountID == "" || instance.Region == "" || instance.ClusterIdentifier == "" {
			continue
		}
		key := clusterKey(instance.AccountID, instance.Region, instance.ClusterIdentifier)
		idx.byCluster[key] = append(idx.byCluster[key], instance)
	}
	return idx
}

// Find returns all instances for a cluster.
func (i *InstanceIndex) Find(accountID, region, clusterIdentifier string) []Instance {
	if i == nil {
		return nil
	}
	return i.byCluster[clusterKey(accountID, region, clusterIdentifier)]
}

func clusterKey(accountID, region, clusterIdentifier string) string {
	return accountID + "|" + region + "|" + clusterIdentifier
}

// ParseInstanceReport parses a Wiz CSV report containing Aurora MySQL instance rows.
func ParseInstanceReport(r io.Reader) ([]Instance, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1

	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse Aurora instance report CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	cols := buildColumnIndex(rows[0])
	var instances []Instance
	for _, row := range rows[1:] {
		nativeType := firstCol(cols, row, "nativeType", "ResourceNativeType", "Native Type")
		if nativeType != "" && nativeType != auroraMySQLInstanceNativeType {
			continue
		}
		instance := Instance{
			ResourceID:        firstCol(cols, row, "externalId", "ResourceExternalId", "ProviderUniqueId", "providerUniqueId", "Provider ID"),
			AccountID:         firstCol(cols, row, "cloudAccount.externalId", "Subscription.ExternalId", "subscriptionExternalId", "AccountID"),
			Region:            firstCol(cols, row, "region", "Region"),
			ClusterIdentifier: firstCol(cols, row, "typeFields.dbClusterIdentifier", "dbClusterIdentifier", "DBClusterIdentifier", "clusterIdentifier", "ClusterIdentifier"),
			DBInstanceClass:   firstCol(cols, row, "typeFields.dbInstanceClass", "dbInstanceClass", "DBInstanceClass", "instanceClass", "InstanceClass"),
			Status:            firstCol(cols, row, "status", "Status", "dbInstanceStatus", "DBInstanceStatus"),
			Engine:            normalizeEngine(firstCol(cols, row, "typeFields.kind", "kind", "Engine", "engine")),
			EngineVersion:     firstCol(cols, row, "versionDetails.version", "engineVersion", "EngineVersion", "version", "Version"),
		}
		if instance.ResourceID == "" && instance.ClusterIdentifier == "" && instance.DBInstanceClass == "" {
			continue
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

func buildColumnIndex(header []string) map[string]int {
	cols := make(map[string]int, len(header)*2)
	for i, name := range header {
		trimmed := strings.TrimSpace(name)
		cols[trimmed] = i
		cols[strings.ToLower(trimmed)] = i
	}
	return cols
}

func firstCol(cols map[string]int, row []string, candidates ...string) string {
	for _, candidate := range candidates {
		for _, key := range []string{candidate, strings.ToLower(candidate)} {
			idx, ok := cols[key]
			if ok && idx >= 0 && idx < len(row) {
				value := strings.TrimSpace(row[idx])
				if value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func normalizeEngine(engine string) string {
	engine = strings.ToLower(strings.TrimSpace(engine))
	if strings.Contains(engine, "aurora") && strings.Contains(engine, "mysql") {
		return "aurora-mysql"
	}
	return engine
}

func isBillableStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "available", "backing-up", "modifying", "configuring-enhanced-monitoring", "storage-optimization":
		return true
	case "stopped", "stopping", "deleting", "deleted", "failed":
		return false
	default:
		return true
	}
}
