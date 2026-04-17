package main

import (
	"context"
	"fmt"
	"log/slog"
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
	"github.com/block/Version-Guard/pkg/detector/generic"
	"github.com/block/Version-Guard/pkg/eol"
	eolendoflife "github.com/block/Version-Guard/pkg/eol/endoflife"
	"github.com/block/Version-Guard/pkg/inventory"
	"github.com/block/Version-Guard/pkg/inventory/wiz"
	"github.com/block/Version-Guard/pkg/policy"
	"github.com/block/Version-Guard/pkg/registry"
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

	// S3 configuration (for snapshots)
	S3Bucket   string `help:"S3 bucket for snapshots" default:"version-guard-snapshots" env:"S3_BUCKET"`
	S3Prefix   string `help:"S3 prefix for snapshots" default:"snapshots/" env:"S3_PREFIX"`
	S3Endpoint string `help:"Custom S3 endpoint (for MinIO/local dev)" env:"S3_ENDPOINT"`

	// Service configuration
	GRPCPort int `help:"gRPC service port" default:"8080" env:"GRPC_PORT"`

	// Tag configuration (comma-separated lists for AWS resource tags)
	TagAppKeys   string `help:"Comma-separated tag keys for application/service name" default:"app,application,service" env:"TAG_APP_KEYS"`
	TagEnvKeys   string `help:"Comma-separated tag keys for environment" default:"environment,env" env:"TAG_ENV_KEYS"`
	TagBrandKeys string `help:"Comma-separated tag keys for brand/business unit" default:"brand" env:"TAG_BRAND_KEYS"`

	// Resource configuration
	ConfigPath string `help:"Path to resources config file" default:"config/resources.yaml" env:"CONFIG_PATH"`

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
		AppTags:   parseTagKeys(s.TagAppKeys),
		EnvTags:   parseTagKeys(s.TagEnvKeys),
		BrandTags: parseTagKeys(s.TagBrandKeys),
	}
}

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
	fmt.Printf("  gRPC Port: %d\n", s.GRPCPort)
	fmt.Printf("  S3 Bucket: %s\n", s.S3Bucket)

	if s.Verbose {
		fmt.Printf("\nDetailed Configuration:\n")
		fmt.Printf("  Temporal Task Queue: %s\n", s.TemporalTaskQueue)
		fmt.Printf("  Wiz Cache TTL: %d hours\n", s.WizCacheTTLHours)
		fmt.Printf("  AWS Region: %s\n", s.AWSRegion)
		fmt.Printf("  S3 Prefix: %s\n", s.S3Prefix)
		fmt.Printf("  Tag Keys - App: %s\n", s.TagAppKeys)
		fmt.Printf("  Tag Keys - Env: %s\n", s.TagEnvKeys)
		fmt.Printf("  Tag Keys - Brand: %s\n", s.TagBrandKeys)
	}

	if s.DryRun {
		fmt.Println("\n⚠️  Running in DRY-RUN mode (workers not started)")
		return nil
	}

	// Initialize store
	st := memory.NewStore()
	fmt.Println("✓ In-memory store initialized")

	// Initialize S3 snapshot store
	var snapshotStore *snapshot.S3Store
	ctx := context.Background()
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

	// Load resource configuration
	fmt.Printf("Loading resource configuration from %s...\n", s.ConfigPath)
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
		fmt.Printf("  Env tags: %v\n", tagConfig.EnvTags)
		fmt.Printf("  Brand tags: %v\n", tagConfig.BrandTags)
	}

	// Initialize Wiz client if credentials provided
	var wizClient *wiz.Client
	if s.WizClientIDSecret != "" && s.WizClientSecretSecret != "" {
		fmt.Println("✓ Wiz credentials configured — using live inventory")
		wizHTTPClient := wiz.NewHTTPClient(s.WizClientIDSecret, s.WizClientSecretSecret)
		wizClient = wiz.NewClient(wizHTTPClient, time.Duration(s.WizCacheTTLHours)*time.Hour)
	} else {
		fmt.Println("⚠️  No Wiz credentials configured — using mock inventory")
		fmt.Println("   To use live data, set WIZ_CLIENT_ID_SECRET and WIZ_CLIENT_SECRET_SECRET")
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

	// Initialize detectors from config
	fmt.Println("\nInitializing detectors from configuration...")
	detectors := make(map[types.ResourceType]interface{})
	invSources := make(map[types.ResourceType]inventory.InventorySource)
	eolProviders := make(map[types.ResourceType]eol.Provider)

	for i := range resourcesConfig.Resources {
		resourceCfg := &resourcesConfig.Resources[i]
		fmt.Printf("  Configuring %s (%s)...\n", resourceCfg.ID, resourceCfg.Type)

		// Create inventory source
		var invSource inventory.InventorySource
		if resourceCfg.Inventory.Source == "wiz" {
			if wizClient == nil {
				// Wiz client not available (no credentials)
				fmt.Printf("    ⚠️  Skipping %s - Wiz credentials not configured\n", resourceCfg.ID)
				continue
			}

			// Create generic inventory source
			invSource = wiz.NewGenericInventorySource(wizClient, resourceCfg, registryClient, logger)
			fmt.Printf("    ✓ Wiz inventory source created (reads from WIZ_REPORT_IDS[%s])\n", resourceCfg.ID)
		} else {
			fmt.Printf("    ⚠️  Unsupported inventory source: %s\n", resourceCfg.Inventory.Source)
			continue
		}

		// Create EOL provider based on config
		var eolProvider eol.Provider
		if resourceCfg.EOL.Provider == "endoflife-date" {
			eolProvider = eolendoflife.NewProvider(eolHTTPClient, cacheTTL, logger)
			fmt.Printf("    ✓ EOL provider created (endoflife.date: %s)\n", resourceCfg.EOL.Product)
		} else {
			fmt.Printf("    ⚠️  Unsupported EOL provider: %s\n", resourceCfg.EOL.Provider)
			continue
		}

		// Store in maps for Temporal activities
		resourceType := types.ResourceType(resourceCfg.Type)
		invSources[resourceType] = invSource
		eolProviders[resourceType] = eolProvider

		// Create generic detector
		detector := generic.NewDetector(resourceCfg, invSource, eolProvider, policyEngine, logger)
		detectors[resourceType] = detector
		fmt.Printf("    ✓ Generic detector initialized for %s\n", resourceCfg.ID)
	}

	if len(detectors) == 0 {
		return fmt.Errorf("no detectors configured - check your config file and Wiz credentials")
	}
	fmt.Printf("\n✓ Total detectors initialized: %d\n", len(detectors))

	// Start gRPC server
	// Note: gRPC server requires protobuf code generation first.
	// Run `make protos` to generate the gRPC service code, then uncomment below:
	//nolint:gocritic // Intentionally commented - template for future gRPC implementation
	/*
		grpcServer := grpc.NewServer()
		vgService := grpcservice.NewService(st)
		// Register service using generated proto code:
		// pb.RegisterVersionGuardServiceServer(grpcServer, vgService)
		reflection.Register(grpcServer) // Enable gRPC reflection for debugging

		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.GRPCPort))
		if err != nil {
			return fmt.Errorf("failed to listen on port %d: %w", s.GRPCPort, err)
		}

		go func() {
			fmt.Printf("✓ gRPC server listening on :%d\n", s.GRPCPort)
			if err := grpcServer.Serve(listener); err != nil {
				fmt.Printf("gRPC server error: %v\n", err)
			}
		}()
	*/
	fmt.Println("⚠️  gRPC server disabled (run 'make protos' to generate proto code)")

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
	// Use first available EOL provider (all resources use endoflife.date which supports multiple engines)
	var firstEOLProvider eol.Provider
	for _, provider := range eolProviders {
		firstEOLProvider = provider
		break
	}
	if firstEOLProvider == nil {
		return fmt.Errorf("no EOL providers configured")
	}

	detectionActivities := detection.NewActivities(
		invSources,
		firstEOLProvider,
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
		fmt.Println("✓ Orchestrator activities registered (with S3)")
	} else {
		fmt.Println("⚠️  Orchestrator snapshot activity not registered (no S3 store)")
	}

	// Start worker
	fmt.Printf("\n✓ Temporal worker starting on queue: %s\n", s.TemporalTaskQueue)
	fmt.Println("\nVersion Guard is ready!")
	fmt.Println("\n📖 To trigger a scan, use the Temporal UI or CLI:")
	fmt.Printf("   temporal workflow start --task-queue %s --type %s --input '{}'\n", s.TemporalTaskQueue, orchestrator.OrchestratorWorkflowType)
	fmt.Println("\n📖 To query findings via gRPC:")
	fmt.Printf("   grpcurl -plaintext localhost:%d list\n", s.GRPCPort)
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
	//nolint:gocritic // Intentionally commented - template for future gRPC implementation
	// grpcServer.GracefulStop() // Uncomment when gRPC server is enabled
	fmt.Println("✓ Shutdown complete")

	return nil
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
