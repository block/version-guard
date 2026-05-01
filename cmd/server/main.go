package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"google.golang.org/grpc"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	vgconfig "github.com/block/Version-Guard/pkg/config"
	"github.com/block/Version-Guard/pkg/eol"
	eolendoflife "github.com/block/Version-Guard/pkg/eol/endoflife"
	"github.com/block/Version-Guard/pkg/inventory"
	mockinv "github.com/block/Version-Guard/pkg/inventory/mock"
	"github.com/block/Version-Guard/pkg/inventory/wiz"
	"github.com/block/Version-Guard/pkg/policy"
	"github.com/block/Version-Guard/pkg/registry"
	"github.com/block/Version-Guard/pkg/scan"
	"github.com/block/Version-Guard/pkg/schedule"
	"github.com/block/Version-Guard/pkg/snapshot"
	"github.com/block/Version-Guard/pkg/store/memory"
	"github.com/block/Version-Guard/pkg/types"
	"github.com/block/Version-Guard/pkg/workflow/detection"
	"github.com/block/Version-Guard/pkg/workflow/orchestrator"
)

var version = "dev"

// ServerCLI defines the server command-line interface
//
//nolint:govet // field alignment sacrificed for logical grouping
type ServerCLI struct {
	// Temporal configuration
	TemporalEndpoint  string `help:"Temporal server endpoint" default:"localhost:7233" env:"TEMPORAL_ENDPOINT"`
	TemporalNamespace string `help:"Temporal namespace" default:"version-guard-dev" env:"TEMPORAL_NAMESPACE"`
	TemporalTaskQueue string `help:"Temporal task queue" default:"version-guard-detection" env:"TEMPORAL_TASK_QUEUE"`

	// Wiz configuration (optional - falls back to mock if not provided)
	WizClientIDSecret      string `help:"Wiz client ID" env:"WIZ_CLIENT_ID_SECRET"`
	WizClientSecretSecret  string `help:"Wiz client secret" env:"WIZ_CLIENT_SECRET_SECRET"`
	WizCacheTTLHours       int    `help:"Wiz cache TTL in hours" default:"1" env:"WIZ_CACHE_TTL_HOURS"`
	WizAuroraReportID      string `help:"Wiz saved report ID for Aurora inventory" env:"WIZ_AURORA_REPORT_ID"`
	WizElastiCacheReportID string `help:"Wiz saved report ID for ElastiCache inventory" env:"WIZ_ELASTICACHE_REPORT_ID"`
	WizEKSReportID         string `help:"Wiz saved report ID for EKS inventory" env:"WIZ_EKS_REPORT_ID"`

	// EOL configuration
	EOLBaseURL string `help:"Custom base URL for endoflife.date API (e.g., http://localhost:8082/api)" env:"EOL_BASE_URL"`

	// AWS configuration (for EOL APIs)
	AWSRegion string `help:"AWS region for EOL APIs" default:"us-west-2" env:"AWS_REGION"`

	// Snapshot storage backend ("s3" or "memory"). "memory" is intended
	// for laptop dev and CI smoke tests; it has no durability across
	// restarts but lets the orchestrator's Stage 2 succeed without AWS
	// credentials.
	SnapshotStore string `help:"Snapshot store backend: s3 or memory" default:"s3" enum:"s3,memory" env:"SNAPSHOT_STORE"`

	// S3 configuration (for snapshots)
	S3Bucket   string `help:"S3 bucket for snapshots" default:"version-guard-snapshots" env:"S3_BUCKET"`
	S3Prefix   string `help:"S3 prefix for snapshots" default:"snapshots/" env:"S3_PREFIX"`
	S3Endpoint string `help:"Custom S3 endpoint (for MinIO/local dev)" env:"S3_ENDPOINT"`

	// Service configuration
	HTTPPort int `help:"HTTP admin port (POST /scan)" default:"8081" env:"HTTP_PORT"`

	// Emitter webhook (Stage 3). When set, OrchestratorWorkflow POSTs to
	// "<url>/trigger-act" after the snapshot is persisted, kicking the
	// downstream emitter immediately instead of waiting for its own cron.
	// Empty disables the webhook (snapshot still lands in S3 for any
	// pull-based consumer).
	EmitterWebhookURL string `help:"Base URL of the emitter webhook (e.g. http://version-guard-emitter:8080)" env:"EMITTER_WEBHOOK_URL"`

	// InventoryFallback selects what to do when a resource is configured
	// to use Wiz inventory but no Wiz credentials are present. Default
	// (empty) preserves the loud, fail-fast behavior: the resource is
	// skipped and startup ultimately errors with "no resources
	// configured". Set to "mock" to substitute a single synthetic
	// resource per config — only intended for laptop dev / CI smoke
	// tests of the detector → snapshot → emitter wire. Never set in
	// production: a missing Wiz secret would otherwise silently emit
	// fabricated 1.0.0 findings into S3 and poison every downstream
	// consumer.
	InventoryFallback string `help:"Fallback inventory source when Wiz creds missing: empty (fail), or 'mock' (dev only)" default:"" enum:",mock" env:"INVENTORY_FALLBACK"`

	// Tag configuration (comma-separated lists for AWS resource tags)
	TagAppKeys string `help:"Comma-separated tag keys for application/service name" default:"app,application,service" env:"TAG_APP_KEYS"`

	// Schedule configuration
	ScheduleEnabled bool   `help:"Enable scheduled scanning" default:"false" env:"SCHEDULE_ENABLED"`
	ScheduleCron    string `help:"Cron expression for scan schedule" default:"0 6 * * *" env:"SCHEDULE_CRON"`
	ScheduleID      string `help:"Temporal schedule ID" default:"version-guard-scan" env:"SCHEDULE_ID"`
	ScheduleJitter  string `help:"Schedule jitter duration" default:"5m" env:"SCHEDULE_JITTER"`

	// Resource configuration. Empty (the default) uses the canonical
	// resources.yaml embedded into the binary at build time. Set to
	// override with a custom YAML file — useful for shipping a
	// different resource catalog, custom field mappings, or alternate
	// EOL providers without rebuilding.
	ConfigPath string `help:"Path to resources config file (empty = use embedded default)" env:"CONFIG_PATH"`

	// Global flags
	Verbose bool `short:"v" help:"Enable verbose logging"`
	DryRun  bool `help:"Run in dry-run mode (no Temporal workers started)"`
}

// parseTagKeys parses a comma-separated string into a slice of tag keys
func parseTagKeys(input string) []string {
	if input == "" {
		return []string{}
	}
	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// buildTagConfig creates a TagConfig from the environment variables
func (s *ServerCLI) buildTagConfig() *wiz.TagConfig {
	return &wiz.TagConfig{
		AppTags: parseTagKeys(s.TagAppKeys),
	}
}

//nolint:gocognit,gocyclo // startup wires many optional components; splitting further would fragment a linear init sequence
func (s *ServerCLI) Run(_ *kong.Context) error {
	// Initialize structured logger
	logLevel := slog.LevelInfo
	if s.Verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	fmt.Println("Starting Version Guard Detector Service (Open Source)")
	fmt.Printf("  Version: %s\n", version)
	fmt.Printf("  Temporal Endpoint: %s\n", s.TemporalEndpoint)
	fmt.Printf("  Temporal Namespace: %s\n", s.TemporalNamespace)
	fmt.Printf("  S3 Bucket: %s\n", s.S3Bucket)

	if s.Verbose {
		fmt.Printf("\nDetailed Configuration:\n")
		fmt.Printf("  Temporal Task Queue: %s\n", s.TemporalTaskQueue)
		fmt.Printf("  Wiz Cache TTL: %d hours\n", s.WizCacheTTLHours)
		fmt.Printf("  AWS Region: %s\n", s.AWSRegion)
		fmt.Printf("  S3 Prefix: %s\n", s.S3Prefix)
		fmt.Printf("  Tag Keys - App: %s\n", s.TagAppKeys)
		if s.ScheduleEnabled {
			fmt.Printf("  Schedule: enabled (cron: %s, id: %s, jitter: %s)\n",
				s.ScheduleCron, s.ScheduleID, s.ScheduleJitter)
		} else {
			fmt.Printf("  Schedule: disabled\n")
		}
	}

	if s.DryRun {
		fmt.Println("\n⚠️  Running in DRY-RUN mode (workers not started)")
		return nil
	}

	// Initialize store
	st := memory.NewStore()
	fmt.Println("✓ In-memory store initialized")

	// Initialize snapshot store. Production runs use S3; laptop dev / CI
	// smoke tests can use the in-memory store via `SNAPSHOT_STORE=memory`
	// to avoid needing AWS credentials.
	var snapshotStore snapshot.Store
	ctx := context.Background()
	if s.SnapshotStore == "memory" {
		snapshotStore = snapshot.NewMemoryStore()
		fmt.Println("✓ In-memory snapshot store initialized (SNAPSHOT_STORE=memory; not durable)")
	} else {
		configOpts := []func(*config.LoadOptions) error{config.WithRegion(s.AWSRegion)}
		cfg, err := config.LoadDefaultConfig(ctx, configOpts...)
		if err != nil {
			fmt.Printf("⚠️  Failed to load AWS config: %v\n", err)
			fmt.Println("   Snapshots will not be persisted to S3")
		} else {
			s3Opts := []func(*s3.Options){}
			if s.S3Endpoint != "" {
				s3Opts = append(s3Opts, func(o *s3.Options) {
					o.BaseEndpoint = &s.S3Endpoint
					o.UsePathStyle = true
				})
			}
			s3Client := s3.NewFromConfig(cfg, s3Opts...)
			snapshotStore = snapshot.NewS3Store(s3Client, s.S3Bucket, s.S3Prefix)
			fmt.Printf("✓ S3 snapshot store initialized (bucket: %s)\n", s.S3Bucket)
		}
	}

	// Initialize Temporal client
	temporalClient, err := client.Dial(client.Options{
		HostPort:  s.TemporalEndpoint,
		Namespace: s.TemporalNamespace,
		ConnectionOptions: client.ConnectionOptions{
			DialOptions: []grpc.DialOption{
				grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(20 * 1024 * 1024)), // 20MB for large Wiz reports
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to connect to Temporal at %s: %w", s.TemporalEndpoint, err)
	}
	defer temporalClient.Close()
	fmt.Printf("✓ Connected to Temporal at %s (namespace: %s)\n", s.TemporalEndpoint, s.TemporalNamespace)

	// Load resource configuration. Empty CONFIG_PATH uses the canonical
	// YAML embedded into the binary; a non-empty path fully replaces it.
	if s.ConfigPath == "" {
		fmt.Println("Loading resource configuration from embedded default...")
	} else {
		fmt.Printf("Loading resource configuration from %s (overrides embedded default)...\n", s.ConfigPath)
	}
	resourcesConfig, err := vgconfig.LoadResourcesConfig(s.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load resources config: %w", err)
	}
	fmt.Printf("✓ Configuration loaded: %d resource(s) defined\n", len(resourcesConfig.Resources))

	// Build tag configuration from environment variables
	tagConfig := s.buildTagConfig()
	if s.Verbose {
		fmt.Printf("\n✓ Tag configuration loaded:\n")
		fmt.Printf("  App tags: %v\n", tagConfig.AppTags)
	}

	// Initialize Wiz client if credentials provided
	var wizClient *wiz.Client
	if s.WizClientIDSecret != "" && s.WizClientSecretSecret != "" {
		fmt.Println("✓ Wiz credentials configured — using live inventory")
		wizHTTPClient := wiz.NewHTTPClient(s.WizClientIDSecret, s.WizClientSecretSecret)
		wizClient = wiz.NewClient(wizHTTPClient, time.Duration(s.WizCacheTTLHours)*time.Hour)
	} else if s.InventoryFallback == "mock" {
		fmt.Println("⚠️  No Wiz credentials configured — INVENTORY_FALLBACK=mock will substitute synthetic resources (DEV ONLY)")
		fmt.Println("   To use live data, set WIZ_CLIENT_ID_SECRET and WIZ_CLIENT_SECRET_SECRET")
	} else {
		fmt.Println("⚠️  No Wiz credentials configured — Wiz-sourced resources will be skipped (startup will fail if none remain)")
		fmt.Println("   To use live data, set WIZ_CLIENT_ID_SECRET and WIZ_CLIENT_SECRET_SECRET")
		fmt.Println("   For local dev/CI smoke tests, set INVENTORY_FALLBACK=mock")
	}

	// Create EOL HTTP client (shared across all resources)
	var eolHTTPClient eolendoflife.Client
	if s.EOLBaseURL != "" {
		fmt.Printf("✓ Using custom EOL API: %s\n", s.EOLBaseURL)
		eolHTTPClient = eolendoflife.NewRealHTTPClientWithConfig(nil, s.EOLBaseURL)
	} else {
		eolHTTPClient = eolendoflife.NewRealHTTPClient()
	}
	cacheTTL := 24 * time.Hour

	// Initialize policy engine (shared across all detectors)
	policyEngine := policy.NewDefaultPolicy()

	// Create registry client (optional, for service lookups)
	var registryClient registry.Client

	// Initialize per-resource inventory sources and EOL providers from
	// config. Both maps are keyed by config ID (not resource type) so
	// multiple resources of the same type (e.g. aurora-postgresql and
	// aurora-mysql both type "aurora") get independent providers and
	// caches. The detection workflow's activities consume both maps —
	// there is no Go-side detector instance anymore; the orchestrator
	// fans out activities directly.
	fmt.Println("\nConfiguring inventory sources and EOL providers...")
	invSources := make(map[types.ResourceType]inventory.InventorySource)
	eolProviders := make(map[types.ResourceType]eol.Provider)

	for i := range resourcesConfig.Resources {
		resourceCfg := &resourcesConfig.Resources[i]
		fmt.Printf("  Configuring %s (%s)...\n", resourceCfg.ID, resourceCfg.Type)

		// Create inventory source
		var invSource inventory.InventorySource
		if resourceCfg.Inventory.Source == "wiz" {
			switch {
			case wizClient != nil:
				// Create generic inventory source
				invSource = wiz.NewGenericInventorySource(wizClient, resourceCfg, registryClient, logger)
				fmt.Printf("    ✓ Wiz inventory source created (reads from WIZ_REPORT_IDS[%s])\n", resourceCfg.ID)
			case s.InventoryFallback == "mock":
				// Explicit dev-only opt-in: substitute a single synthetic
				// resource so local e2e runs (webhook smoke tests etc.) can
				// exercise the full detector → snapshot → emitter wire
				// without CloudSec-issued Wiz credentials. Engine is keyed
				// off resourceCfg.ID (e.g. "aurora-postgresql") so the
				// endoflife.date adapters resolve a real lifecycle instead
				// of producing UNKNOWN.
				configID := types.ResourceType(resourceCfg.ID)
				invSource = &mockinv.InventorySource{
					Resources: []*types.Resource{
						{
							ID:             fmt.Sprintf("mock-%s-1", resourceCfg.ID),
							Service:        "version-guard-mock",
							Type:           configID,
							CurrentVersion: "1.0.0",
							Engine:         resourceCfg.ID,
							CloudProvider:  types.CloudProviderAWS,
							DiscoveredAt:   time.Now(),
							Tags:           map[string]string{"env": "local-dev"},
						},
					},
				}
				fmt.Printf("    ⚠️  %s - INVENTORY_FALLBACK=mock; using 1 fake resource (DEV ONLY)\n", resourceCfg.ID)
			default:
				// No Wiz credentials and no explicit mock opt-in: skip and
				// let startup fail loudly with "no resources configured".
				// This protects production from silently emitting
				// fabricated 1.0.0 findings if a Wiz secret is missing.
				fmt.Printf("    ⚠️  %s - Wiz credentials not configured; skipping (set INVENTORY_FALLBACK=mock for dev)\n", resourceCfg.ID)
				continue
			}
		} else {
			fmt.Printf("    ⚠️  Unsupported inventory source: %s\n", resourceCfg.Inventory.Source)
			continue
		}

		// Create EOL provider based on config
		var eolProvider eol.Provider
		if resourceCfg.EOL.Provider == "endoflife-date" {
			provider, err := eolendoflife.NewProviderWithLifecycle(
				eolHTTPClient,
				resourceCfg.EOL.Product,
				resourceCfg.EOL.Schema,
				resourceCfg.EOL.Lifecycle,
				cacheTTL,
				logger,
			)
			if err != nil {
				return fmt.Errorf("failed to create EOL provider for %s: %w", resourceCfg.ID, err)
			}
			eolProvider = provider
			schemaLabel := resourceCfg.EOL.Schema
			if schemaLabel == "" {
				schemaLabel = "standard"
			}
			fmt.Printf("    ✓ EOL provider created (endoflife.date: %s, schema: %s)\n",
				resourceCfg.EOL.Product, schemaLabel)
		} else {
			fmt.Printf("    ⚠️  Unsupported EOL provider: %s\n", resourceCfg.EOL.Provider)
			continue
		}

		// Store in maps for Temporal activities, keyed by config ID
		configID := types.ResourceType(resourceCfg.ID)
		invSources[configID] = invSource
		eolProviders[configID] = eolProvider
		fmt.Printf("    ✓ %s configured\n", resourceCfg.ID)
	}

	if len(invSources) == 0 {
		return fmt.Errorf("no resources configured - check your config file and Wiz credentials")
	}
	fmt.Printf("\n✓ Total resources configured: %d\n", len(invSources))

	// Build the canonical list of resource types to scan from the loaded
	// YAML config. We iterate resourcesConfig.Resources (YAML order is
	// stable, unlike map iteration) and only include entries that
	// successfully initialized — Wiz-credentials-skipped resources stay
	// out of scheduled runs. This list is the single source of truth
	// for "full fleet scan" everywhere downstream: scheduled triggers,
	// HTTP-triggered triggers, and any future entry point. The
	// orchestrator workflow rejects empty input (see
	// orchestrator.ErrNoResourceTypes), so adding a resource is a
	// YAML-only change with no Go-side hardcoded list to drift from.
	defaultResourceTypes := make([]types.ResourceType, 0, len(resourcesConfig.Resources))
	for i := range resourcesConfig.Resources {
		configID := types.ResourceType(resourcesConfig.Resources[i].ID)
		if _, ok := invSources[configID]; ok {
			defaultResourceTypes = append(defaultResourceTypes, configID)
		}
	}
	if s.Verbose {
		fmt.Printf("  Default scan list: %v\n", defaultResourceTypes)
	}

	// Create Temporal worker
	w := worker.New(temporalClient, s.TemporalTaskQueue, worker.Options{
		EnableSessionWorker: true,
	})

	// Register workflows
	w.RegisterWorkflow(detection.DetectionWorkflow)
	w.RegisterWorkflow(orchestrator.OrchestratorWorkflow)
	fmt.Println("✓ Workflows registered (detection, orchestrator)")

	// Register activities
	// Detection workflow activities
	if len(eolProviders) == 0 {
		return fmt.Errorf("no EOL providers configured")
	}

	// Pass the per-resource-type provider map. FetchEOLData routes to the
	// correct Provider via FetchEOLInput.ResourceType. Picking a single
	// provider here would cause every other resource type's queries to
	// hit the wrong endoflife.date product and silently produce UNKNOWN
	// findings.
	detectionActivities := detection.NewActivities(
		invSources,
		eolProviders,
		policyEngine,
		st,
	)
	w.RegisterActivityWithOptions(detectionActivities.FetchInventory, activity.RegisterOptions{Name: detection.FetchInventoryActivityName})
	w.RegisterActivityWithOptions(detectionActivities.FetchEOLData, activity.RegisterOptions{Name: detection.FetchEOLDataActivityName})
	w.RegisterActivityWithOptions(detectionActivities.DetectDrift, activity.RegisterOptions{Name: detection.DetectDriftActivityName})
	w.RegisterActivityWithOptions(detectionActivities.StoreFindings, activity.RegisterOptions{Name: detection.StoreFindingsActivityName})
	w.RegisterActivityWithOptions(detectionActivities.EmitMetrics, activity.RegisterOptions{Name: detection.EmitMetricsActivityName})
	fmt.Println("✓ Detection activities registered")

	// Orchestrator workflow activities
	if snapshotStore != nil {
		orchestratorActivities := orchestrator.NewActivities(st, snapshotStore)
		w.RegisterActivityWithOptions(orchestratorActivities.CreateSnapshot, activity.RegisterOptions{Name: orchestrator.CreateSnapshotActivityName})
		w.RegisterActivityWithOptions(orchestratorActivities.NotifyEmitter, activity.RegisterOptions{Name: orchestrator.NotifyEmitterActivityName})
		fmt.Println("✓ Orchestrator activities registered (with S3)")
	} else {
		fmt.Println("⚠️  Orchestrator snapshot activity not registered (no S3 store)")
	}

	// Create schedule (if enabled)
	if s.ScheduleEnabled {
		s.ensureSchedule(ctx, temporalClient, defaultResourceTypes)
	}

	// Start HTTP admin server (POST /scan to trigger manual scans)
	httpServer := startAdminHTTPServer(s.HTTPPort, temporalClient, s.TemporalTaskQueue, defaultResourceTypes, s.EmitterWebhookURL)

	if s.EmitterWebhookURL != "" {
		fmt.Printf("✓ Emitter webhook configured: %s/trigger-act\n", strings.TrimRight(s.EmitterWebhookURL, "/"))
	}

	// Start worker
	fmt.Printf("\n✓ Temporal worker starting on queue: %s\n", s.TemporalTaskQueue)
	fmt.Println("\nVersion Guard is ready!")
	if s.ScheduleEnabled {
		fmt.Printf("   Scans will run automatically (schedule: %s)\n", s.ScheduleCron)
	}
	fmt.Println("\n📖 To trigger a scan manually, use the Temporal UI or CLI:")
	fmt.Printf("   temporal workflow start --task-queue %s --type %s --input '{}'\n", s.TemporalTaskQueue, orchestrator.OrchestratorWorkflowType)
	fmt.Println("\n📖 For more information, see the README.md")
	fmt.Println("\nPress Ctrl+C to stop...")

	if err := w.Start(); err != nil {
		return fmt.Errorf("failed to start worker: %w", err)
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n\nShutting down gracefully...")
	w.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		fmt.Printf("HTTP server shutdown error: %v\n", err)
	}

	fmt.Println("✓ Shutdown complete")

	return nil
}

// ensureSchedule creates or updates the Temporal schedule for periodic scans.
// Failures are logged but do not abort startup; the worker can still service
// manual scans triggered via the HTTP endpoint or CLI.
func (s *ServerCLI) ensureSchedule(ctx context.Context, temporalClient client.Client, defaultResourceTypes []types.ResourceType) {
	jitter, parseErr := time.ParseDuration(s.ScheduleJitter)
	if parseErr != nil {
		fmt.Printf("⚠️  Invalid schedule jitter %q, using default 5m: %v\n", s.ScheduleJitter, parseErr)
		jitter = 5 * time.Minute
	}

	scheduleMgr := schedule.NewManager(temporalClient)
	schedCtx, schedCancel := context.WithTimeout(ctx, 10*time.Second)
	defer schedCancel()
	schedErr := scheduleMgr.EnsureSchedule(schedCtx, schedule.Config{
		Enabled:           true,
		ScheduleID:        s.ScheduleID,
		CronExpression:    s.ScheduleCron,
		Jitter:            jitter,
		TaskQueue:         s.TemporalTaskQueue,
		ResourceTypes:     defaultResourceTypes,
		EmitterWebhookURL: s.EmitterWebhookURL,
	})
	if schedErr != nil {
		fmt.Printf("⚠️  Failed to create/update schedule: %v\n", schedErr)
		fmt.Println("   Worker will continue — trigger scans manually")
		return
	}
	fmt.Printf("✓ Schedule configured: %s (cron: %s, jitter: %s)\n",
		s.ScheduleID, s.ScheduleCron, s.ScheduleJitter)
}

// startAdminHTTPServer wires the scan trigger into an HTTP admin server and
// starts listening in a background goroutine. The returned *http.Server can be
// shut down gracefully by the caller.
func startAdminHTTPServer(port int, temporalClient client.Client, taskQueue string, defaultResourceTypes []types.ResourceType, emitterWebhookURL string) *http.Server {
	scanTrigger := scan.NewTrigger(temporalClient, taskQueue, defaultResourceTypes).
		WithEmitterWebhookURL(emitterWebhookURL)
	mux := http.NewServeMux()
	mux.Handle("/scan", scan.NewHandler(scanTrigger))

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		fmt.Printf("✓ HTTP admin server listening on :%d (POST /scan)\n", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()

	return srv
}

func main() {
	var cli ServerCLI
	kongCtx := kong.Parse(&cli,
		kong.Name("version-guard"),
		kong.Description("Version Guard - Cloud infrastructure version monitoring"),
		kong.UsageOnError(),
	)

	err := kongCtx.Run(&cli)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
