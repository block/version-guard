package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/block/Version-Guard/pkg/types"
)

const (
	// SnapshotSchemaVersion is the current schema version for snapshots.
	//
	// v3 (current): removed Finding.Recommendation from the snapshot schema;
	//   remediation guidance belongs in curated docs, not in generated
	//   findings. Finding.lifecycle_details is required metadata added for
	//   downstream enrichment and remains backward-compatible within v3.
	// v2 (deprecated): tightened the typed Finding surface to only the
	//   fields the system itself requires (identity, EOL keys, service,
	//   classification metadata, tags). Top-level resource_name,
	//   cloud_account_id, and cloud_region keys were removed; their
	//   values moved into Finding.Extra under YAML logical names
	//   ("name", "account_id", "region").
	// v1 (deprecated): typed Finding included resource_name,
	//   cloud_account_id, and cloud_region as top-level keys.
	SnapshotSchemaVersion = "v3"
)

// Store handles persisting snapshots to S3
type Store interface {
	// SaveSnapshot writes a snapshot to S3 with versioning
	SaveSnapshot(ctx context.Context, snapshot *types.Snapshot) error

	// GetLatestSnapshot retrieves the most recent snapshot
	GetLatestSnapshot(ctx context.Context) (*types.Snapshot, error)

	// GetSnapshot retrieves a specific snapshot by ID
	GetSnapshot(ctx context.Context, snapshotID string) (*types.Snapshot, error)

	// ListSnapshots lists recent snapshots with optional limit
	ListSnapshots(ctx context.Context, limit int) ([]*Metadata, error)
}

// Metadata provides summary information about a snapshot without loading full content.
//
//nolint:govet // field alignment sacrificed for logical grouping
type Metadata struct {
	SnapshotID           string
	GeneratedAt          time.Time
	TotalResources       int
	CompliancePercentage float64
	S3Key                string
	S3VersionID          string
}

// s3API is the subset of *s3.Client that S3Store actually uses.
// Defined as an interface so unit tests can swap in a fake without
// reaching for the AWS SDK middleware harness or a real bucket.
// *s3.Client satisfies this interface implicitly.
type s3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

// S3Store implements Store using AWS S3
type S3Store struct {
	client s3API
	bucket string
	prefix string // e.g., "version-guard/snapshots/"
}

// NewS3Store creates a new S3-backed snapshot store
func NewS3Store(client *s3.Client, bucket, prefix string) *S3Store {
	return &S3Store{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}
}

// SaveSnapshot writes a snapshot to S3
func (s *S3Store) SaveSnapshot(ctx context.Context, snapshot *types.Snapshot) error {
	// Generate S3 key with timestamp and snapshot ID
	key := s.generateKey(snapshot.GeneratedAt, snapshot.SnapshotID)

	// Marshal to JSON
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Write to S3 with versioning enabled
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
		Metadata: map[string]string{
			"snapshot-id":           snapshot.SnapshotID,
			"schema-version":        snapshot.Version,
			"total-resources":       fmt.Sprintf("%d", snapshot.Summary.TotalResources),
			"compliance-percentage": fmt.Sprintf("%.2f", snapshot.Summary.CompliancePercentage),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to write snapshot to S3: %w", err)
	}

	// Also write to "latest" key for easy access
	latestKey := s.prefix + "latest.json"
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(latestKey),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
		Metadata: map[string]string{
			"snapshot-id":    snapshot.SnapshotID,
			"schema-version": snapshot.Version,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to write latest snapshot: %w", err)
	}

	return nil
}

// GetLatestSnapshot retrieves the most recent snapshot
func (s *S3Store) GetLatestSnapshot(ctx context.Context) (*types.Snapshot, error) {
	key := s.prefix + "latest.json"

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get latest snapshot from S3: %w", err)
	}
	defer result.Body.Close()

	var snapshot types.Snapshot
	if err := json.NewDecoder(result.Body).Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("failed to decode snapshot: %w", err)
	}

	return &snapshot, nil
}

// GetSnapshot retrieves a specific snapshot by ID
func (s *S3Store) GetSnapshot(ctx context.Context, snapshotID string) (*types.Snapshot, error) {
	// List objects with prefix matching snapshot ID pattern, handling pagination
	var targetKey string
	var continuationToken *string

	for {
		listResult, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.bucket),
			Prefix:            aws.String(s.prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list snapshots: %w", err)
		}

		// Find the matching snapshot in this page
		for _, obj := range listResult.Contents {
			// Check if key contains the snapshot ID
			if obj.Key != nil && containsSnapshotID(*obj.Key, snapshotID) {
				targetKey = *obj.Key
				break
			}
		}

		// If found, stop paginating
		if targetKey != "" {
			break
		}

		// Check if there are more pages
		if listResult.IsTruncated != nil && *listResult.IsTruncated {
			continuationToken = listResult.NextContinuationToken
		} else {
			// No more pages and not found
			break
		}
	}

	if targetKey == "" {
		return nil, fmt.Errorf("snapshot not found: %s", snapshotID)
	}

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(targetKey),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshot from S3: %w", err)
	}
	defer result.Body.Close()

	var snapshot types.Snapshot
	if err := json.NewDecoder(result.Body).Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("failed to decode snapshot: %w", err)
	}

	return &snapshot, nil
}

// ListSnapshots lists recent snapshots
func (s *S3Store) ListSnapshots(ctx context.Context, limit int) ([]*Metadata, error) {
	var metadata []*Metadata
	var continuationToken *string
	remaining := limit

	for {
		// Determine how many keys to request in this page (max 1000 per S3 API limit).
		// Cap before the int->int32 conversion so we never overflow on huge limit values.
		pageSize := remaining
		if pageSize > 1000 {
			pageSize = 1000
		}
		maxKeys := int32(pageSize) //nolint:gosec // G115: pageSize is bounded to [1, 1000] above

		listResult, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.bucket),
			Prefix:            aws.String(s.prefix),
			MaxKeys:           aws.Int32(maxKeys),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list snapshots: %w", err)
		}

		for i := range listResult.Contents {
			meta := s.headSnapshotObject(ctx, &listResult.Contents[i])
			if meta == nil {
				continue
			}
			metadata = append(metadata, meta)

			// Check if we've reached the limit
			remaining--
			if remaining <= 0 {
				return metadata, nil
			}
		}

		// Check if there are more pages
		if listResult.IsTruncated != nil && *listResult.IsTruncated && remaining > 0 {
			continuationToken = listResult.NextContinuationToken
		} else {
			// No more pages or reached limit
			break
		}
	}

	return metadata, nil
}

// headSnapshotObject loads the per-object metadata for a single S3
// object surfaced by ListObjectsV2. Returns nil to signal the object
// should be skipped (the "latest" pointer, missing key, or HEAD
// failure) — listing is best-effort and one bad object should not fail
// the whole call.
func (s *S3Store) headSnapshotObject(ctx context.Context, obj *s3types.Object) *Metadata {
	if obj.Key == nil {
		return nil
	}

	// Skip the "latest" pointer
	if *obj.Key == s.prefix+"latest.json" {
		return nil
	}

	headResult, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    obj.Key,
	})
	if err != nil {
		return nil // Skip on error
	}

	meta := &Metadata{
		S3Key:       *obj.Key,
		S3VersionID: aws.ToString(headResult.VersionId),
	}

	// Parse metadata
	if val, ok := headResult.Metadata["snapshot-id"]; ok {
		meta.SnapshotID = val
	}
	if val, ok := headResult.Metadata["total-resources"]; ok {
		// Ignore scan error - if parsing fails, field remains zero
		//nolint:errcheck // Intentionally ignoring parse errors - metadata is best-effort
		fmt.Sscanf(val, "%d", &meta.TotalResources)
	}
	if val, ok := headResult.Metadata["compliance-percentage"]; ok {
		// Ignore scan error - if parsing fails, field remains zero
		//nolint:errcheck // Intentionally ignoring parse errors - metadata is best-effort
		fmt.Sscanf(val, "%f", &meta.CompliancePercentage)
	}

	if obj.LastModified != nil {
		meta.GeneratedAt = *obj.LastModified
	}

	return meta
}

// generateKey creates an S3 key for a snapshot
// Format: {prefix}YYYY/MM/DD/{snapshotID}.json
func (s *S3Store) generateKey(timestamp time.Time, snapshotID string) string {
	return fmt.Sprintf("%s%s/%s.json",
		s.prefix,
		timestamp.Format("2006/01/02"),
		snapshotID,
	)
}

// containsSnapshotID checks if an S3 key contains the given snapshot ID
func containsSnapshotID(key, snapshotID string) bool {
	expectedSuffix := snapshotID + ".json"
	// Check if key is long enough to contain the suffix
	if len(key) < len(expectedSuffix) {
		return false
	}
	return key[len(key)-len(expectedSuffix):] == expectedSuffix
}
