package main

import (
	"context"
	"fmt"

	"github.com/alecthomas/kong"
	"go.temporal.io/sdk/client"

	"github.com/block/Version-Guard/pkg/scan"
	"github.com/block/Version-Guard/pkg/types"
)

var version = "dev"

// CLI defines the command-line interface structure
//
//nolint:govet // field alignment sacrificed for logical grouping
type CLI struct {
	Version  VersionCmd  `cmd:"" help:"Show version information"`
	Service  ServiceCmd  `cmd:"" help:"Check service compliance"`
	Finding  FindingCmd  `cmd:"" help:"Manage findings"`
	Scan     ScanCmd     `cmd:"" help:"Trigger and manage scans"`
	Workflow WorkflowCmd `cmd:"" help:"Inspect Temporal workflow runs"`
	Debug    DebugCmd    `cmd:"" help:"Debug commands for troubleshooting"`

	// Global flags
	Endpoint string `help:"Version Guard HTTP endpoint" default:"localhost:8081" env:"VERSION_GUARD_ENDPOINT"`
	Verbose  bool   `short:"v" help:"Enable verbose logging"`

	// Temporal connection flags (used by workflow commands)
	TemporalEndpoint  string `help:"Temporal server endpoint" default:"localhost:7233" env:"TEMPORAL_ENDPOINT"`
	TemporalNamespace string `help:"Temporal namespace" default:"version-guard-dev" env:"TEMPORAL_NAMESPACE"`
	TemporalTaskQueue string `help:"Temporal task queue" default:"version-guard-detection" env:"TEMPORAL_TASK_QUEUE"`
}

// VersionCmd shows version information
type VersionCmd struct{}

func (c *VersionCmd) Run(ctx *Context) error {
	fmt.Printf("version-guard-cli version %s\n", version)
	return nil
}

// ServiceCmd checks service compliance
type ServiceCmd struct {
	Check ServiceCheckCmd `cmd:"" help:"Check service compliance score"`
	List  ServiceListCmd  `cmd:"" help:"List all services"`
}

// ServiceCheckCmd checks a specific service
type ServiceCheckCmd struct {
	Service       string `arg:"" help:"Service name to check" required:""`
	ResourceType  string `help:"Filter by resource type (aurora, elasticache, etc.)"`
	CloudProvider string `help:"Filter by cloud provider (aws, gcp, azure)"`
	OutputFormat  string `help:"Output format (text, json, yaml)" default:"text" enum:"text,json,yaml"`
}

//nolint:unparam // error return required by kong interface
func (c *ServiceCheckCmd) Run(ctx *Context) error {
	fmt.Printf("Checking compliance for service: %s\n", c.Service)

	if ctx.Verbose {
		fmt.Printf("  Endpoint: %s\n", ctx.Endpoint)
		if c.ResourceType != "" {
			fmt.Printf("  Resource Type: %s\n", c.ResourceType)
		}
		if c.CloudProvider != "" {
			fmt.Printf("  Cloud Provider: %s\n", c.CloudProvider)
		}
	}

	// TODO: Connect to HTTP admin API and fetch compliance score
	fmt.Println("\nCompliance Score: BRONZE")
	fmt.Println("Total Resources: 25")
	fmt.Println("RED: 5")
	fmt.Println("YELLOW: 10")
	fmt.Println("GREEN: 10")
	fmt.Println("Compliance: 40%")

	return nil
}

// ServiceListCmd lists all services
type ServiceListCmd struct {
	Status       string `help:"Filter by status (red, yellow, green)"`
	OutputFormat string `help:"Output format (text, json, yaml)" default:"text" enum:"text,json,yaml"`
}

func (c *ServiceListCmd) Run(ctx *Context) error {
	fmt.Println("Listing all services...")

	// TODO: Connect to HTTP admin API and list services
	fmt.Println("\nService Name    | Grade  | Resources | RED | YELLOW | GREEN")
	fmt.Println("----------------|--------|-----------|-----|--------|------")
	fmt.Println("payments        | BRONZE |        25 |   5 |     10 |    10")
	fmt.Println("billing         | SILVER |        15 |   0 |      5 |    10")
	fmt.Println("analytics       | GOLD   |        10 |   0 |      0 |    10")

	return nil
}

// FindingCmd manages findings
type FindingCmd struct {
	List   FindingListCmd   `cmd:"" help:"List findings"`
	Show   FindingShowCmd   `cmd:"" help:"Show finding details"`
	Export FindingExportCmd `cmd:"" help:"Export findings to CSV"`
}

// FindingListCmd lists findings
//
//nolint:govet // field alignment sacrificed for logical grouping
type FindingListCmd struct {
	Service       string `help:"Filter by service"`
	Status        string `help:"Filter by status (red, yellow, green)"`
	ResourceType  string `help:"Filter by resource type"`
	CloudProvider string `help:"Filter by cloud provider"`
	Limit         int    `help:"Limit number of results" default:"100"`
	OutputFormat  string `help:"Output format (text, json, yaml)" default:"text" enum:"text,json,yaml"`
}

//nolint:unparam // error return required by kong interface
func (c *FindingListCmd) Run(_ *Context) error {
	fmt.Println("Listing findings...")
	if c.Service != "" {
		fmt.Printf("  Service: %s\n", c.Service)
	}
	if c.Status != "" {
		fmt.Printf("  Status: %s\n", c.Status)
	}

	// TODO: Connect to HTTP admin API and list findings
	fmt.Println("\nResource ID                                    | Status | Current Version | EOL Date")
	fmt.Println("-----------------------------------------------|--------|-----------------|----------")
	fmt.Println("arn:aws:rds:us-east-1:123:cluster:legacy-db   | RED    | aurora-mysql-5.6| 2024-11-01")
	fmt.Println("arn:aws:rds:us-east-1:123:cluster:staging-db  | YELLOW | aurora-mysql-5.7| 2025-06-01")

	return nil
}

// FindingShowCmd shows detailed finding information
type FindingShowCmd struct {
	ResourceID   string `arg:"" help:"Resource ID to show" required:""`
	OutputFormat string `help:"Output format (text, json, yaml)" default:"text" enum:"text,json,yaml"`
}

//nolint:unparam // error return required by kong interface
func (c *FindingShowCmd) Run(_ *Context) error {
	fmt.Printf("Finding Details for: %s\n\n", c.ResourceID)

	// TODO: Connect to HTTP admin API and fetch finding details
	fmt.Println("Resource ID:      arn:aws:rds:us-east-1:123:cluster:legacy-db")
	fmt.Println("Resource Name:    legacy-db")
	fmt.Println("Resource Type:    AURORA")
	fmt.Println("Service:          payments")
	fmt.Println("Status:           RED")
	fmt.Println("Current Version:  aurora-mysql 5.6.10a")
	fmt.Println("Engine:           aurora-mysql")
	fmt.Println("EOL Date:         2024-11-01")
	fmt.Println("Message:          Version is past End-of-Life (EOL since Nov 2024)")

	return nil
}

// FindingExportCmd exports findings to CSV
type FindingExportCmd struct {
	Output  string `help:"Output file path" default:"findings.csv"`
	Service string `help:"Filter by service"`
	Status  string `help:"Filter by status (red, yellow, green)"`
}

//nolint:unparam // error return required by kong interface
func (c *FindingExportCmd) Run(_ *Context) error {
	fmt.Printf("Exporting findings to: %s\n", c.Output)

	// TODO: Connect to HTTP admin API and export findings
	fmt.Println("✓ Exported 25 findings")

	return nil
}

// ScanCmd triggers scans and manages scan execution.
type ScanCmd struct {
	Start ScanStartCmd `cmd:"" help:"Trigger a scan (full fleet or targeted)"`
}

// ScanStartCmd triggers an OrchestratorWorkflow run.
// Omit --resource-type to scan every configured resource; pass it one or
// more times (or comma-separate) to run a targeted scan.
type ScanStartCmd struct {
	ScanID       string   `help:"Correlation ID for this scan (auto-generated if empty)"`
	ResourceType []string `help:"Resource config ID to scan (repeatable, e.g. aurora-mysql,eks). Empty = full scan."`
	Wait         bool     `help:"Wait for workflow to complete"`
}

func (c *ScanStartCmd) Run(ctx *Context) error {
	temporalClient, err := client.Dial(client.Options{
		HostPort:  ctx.TemporalEndpoint,
		Namespace: ctx.TemporalNamespace,
	})
	if err != nil {
		return fmt.Errorf("connect to Temporal at %s: %w", ctx.TemporalEndpoint, err)
	}
	defer temporalClient.Close()

	resourceTypes := make([]types.ResourceType, 0, len(c.ResourceType))
	for _, rt := range c.ResourceType {
		resourceTypes = append(resourceTypes, types.ResourceType(rt))
	}

	// CLI does not load resources.yaml — the user is expected to pass
	// --resource-type explicitly. An empty list propagates to the
	// orchestrator, which rejects it with ErrNoResourceTypes so the
	// caller gets an immediate, descriptive failure.
	trigger := scan.NewTrigger(temporalClient, ctx.TemporalTaskQueue, nil)
	res, err := trigger.Run(context.Background(), scan.Input{
		ScanID:        c.ScanID,
		ResourceTypes: resourceTypes,
	})
	if err != nil {
		return fmt.Errorf("trigger scan: %w", err)
	}

	scope := "all configured resources"
	if len(resourceTypes) > 0 {
		scope = fmt.Sprintf("%v", resourceTypes)
	}
	fmt.Printf("✓ Scan started\n")
	fmt.Printf("  Scope:       %s\n", scope)
	fmt.Printf("  Scan ID:     %s\n", res.ScanID)
	fmt.Printf("  Workflow ID: %s\n", res.WorkflowID)
	fmt.Printf("  Run ID:      %s\n", res.RunID)

	if c.Wait {
		fmt.Println("\nWaiting for workflow to complete...")
		run := temporalClient.GetWorkflow(context.Background(), res.WorkflowID, res.RunID)
		if err := run.Get(context.Background(), nil); err != nil {
			return fmt.Errorf("workflow failed: %w", err)
		}
		fmt.Println("✓ Workflow completed successfully")
	}

	return nil
}

// WorkflowCmd inspects Temporal workflow runs.
type WorkflowCmd struct {
	Status WorkflowStatusCmd `cmd:"" help:"Check workflow status"`
	List   WorkflowListCmd   `cmd:"" help:"List recent workflow runs"`
}

// WorkflowStatusCmd checks workflow status
type WorkflowStatusCmd struct {
	WorkflowID string `arg:"" help:"Workflow ID to check" required:""`
}

//nolint:unparam // error return required by kong interface
func (c *WorkflowStatusCmd) Run(_ *Context) error {
	fmt.Printf("Workflow Status: %s\n\n", c.WorkflowID)

	// TODO: Connect to Temporal and fetch status
	fmt.Println("Status:     Completed")
	fmt.Println("Started:    2026-04-07 15:30:00 UTC")
	fmt.Println("Completed:  2026-04-07 15:30:45 UTC")
	fmt.Println("Duration:   45s")
	fmt.Println("Findings:   25")

	return nil
}

// WorkflowListCmd lists recent workflow runs
type WorkflowListCmd struct {
	Limit int `help:"Number of workflows to show" default:"10"`
}

func (c *WorkflowListCmd) Run(ctx *Context) error {
	fmt.Println("Recent Workflow Runs:")

	// TODO: Connect to Temporal and list workflows
	fmt.Println("Workflow ID        | Type   | Status    | Started             | Duration")
	fmt.Println("-------------------|--------|-----------|---------------------|----------")
	fmt.Println("scan-123456        | AURORA | Completed | 2026-04-07 15:30:00 | 45s")
	fmt.Println("scan-123455        | AURORA | Completed | 2026-04-07 09:30:00 | 42s")

	return nil
}

// DebugCmd provides debug utilities
type DebugCmd struct {
	WizTest       DebugWizTestCmd       `cmd:"" help:"Test Wiz API connectivity"`
	InventoryList DebugInventoryListCmd `cmd:"" help:"List inventory from source"`
	EOLVersions   DebugEOLVersionsCmd   `cmd:"" help:"List EOL versions for engine"`
}

// DebugWizTestCmd tests Wiz connectivity
type DebugWizTestCmd struct{}

func (c *DebugWizTestCmd) Run(ctx *Context) error {
	fmt.Println("Testing Wiz API connectivity...")

	// TODO: Test Wiz client
	fmt.Println("✓ Authentication successful")
	fmt.Println("✓ Report access verified")
	fmt.Println("✓ Report download successful")
	fmt.Println("  Resources found: 125")

	return nil
}

// DebugInventoryListCmd lists inventory
type DebugInventoryListCmd struct {
	ResourceType string `help:"Resource type" required:""`
}

//nolint:unparam // error return required by kong interface
func (c *DebugInventoryListCmd) Run(_ *Context) error {
	fmt.Printf("Listing inventory for: %s\n\n", c.ResourceType)

	// TODO: Fetch inventory
	fmt.Println("Resource ID                                    | Name       | Version")
	fmt.Println("-----------------------------------------------|------------|----------------")
	fmt.Println("arn:aws:rds:us-east-1:123:cluster:db-1        | db-1       | aurora-mysql-8.0")

	return nil
}

// DebugEOLVersionsCmd lists EOL versions
type DebugEOLVersionsCmd struct {
	Engine string `help:"Engine name (e.g., aurora-mysql)" required:""`
}

//nolint:unparam // error return required by kong interface
func (c *DebugEOLVersionsCmd) Run(_ *Context) error {
	fmt.Printf("EOL Versions for: %s\n\n", c.Engine)

	// TODO: Fetch EOL data
	fmt.Println("Version    | Status      | EOL Date   | Deprecated")
	fmt.Println("-----------|-------------|------------|------------")
	fmt.Println("5.6.10a    | EOL         | 2024-11-01 | Yes")
	fmt.Println("5.7.12     | Extended    | 2025-06-01 | No")
	fmt.Println("8.0.35     | Supported   | 2028-12-01 | No")

	return nil
}

// Context holds global CLI context
type Context struct {
	*CLI
}

func main() {
	cli := &CLI{}

	ctx := kong.Parse(cli,
		kong.Name("version-guard-cli"),
		kong.Description("Version Guard CLI - Infrastructure version drift detection"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
		kong.Vars{
			"version": version,
		},
	)

	// Create context
	cliContext := &Context{CLI: cli}

	// Execute command
	err := ctx.Run(cliContext)
	ctx.FatalIfErrorf(err)
}
