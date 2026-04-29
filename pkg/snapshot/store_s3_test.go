package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/Version-Guard/pkg/types"
)

// fakeS3 is an in-memory s3API for tests. Stores objects keyed by S3
// key (not by bucket; the tests only ever use one bucket).
type fakeS3 struct {
	objects        map[string]fakeObject     // key -> object
	headOnly       map[string]fakeObject     // key -> metadata-only (HeadObject path); falls back to objects
	listError      error                     // ListObjectsV2 returns this if non-nil
	putError       error                     // PutObject returns this if non-nil
	getError       error                     // GetObject returns this if non-nil
	headError      error                     // HeadObject returns this if non-nil
	listPages      []*s3.ListObjectsV2Output // optional: replay these responses verbatim, in order
	listPageCursor int
}

//nolint:govet // field alignment sacrificed for logical grouping (test-only)
type fakeObject struct {
	body         []byte
	metadata     map[string]string
	versionID    string
	lastModified time.Time
}

func newFakeS3() *fakeS3 {
	return &fakeS3{
		objects:  make(map[string]fakeObject),
		headOnly: make(map[string]fakeObject),
	}
}

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if f.putError != nil {
		return nil, f.putError
	}
	body, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.objects[aws.ToString(in.Key)] = fakeObject{
		body:         body,
		metadata:     in.Metadata,
		versionID:    "v-" + aws.ToString(in.Key),
		lastModified: time.Now(),
	}
	return &s3.PutObjectOutput{VersionId: aws.String("v-" + aws.ToString(in.Key))}, nil
}

func (f *fakeS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if f.getError != nil {
		return nil, f.getError
	}
	obj, ok := f.objects[aws.ToString(in.Key)]
	if !ok {
		return nil, errors.New("NoSuchKey")
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(obj.body))}, nil
}

func (f *fakeS3) ListObjectsV2(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if f.listError != nil {
		return nil, f.listError
	}
	// If pre-canned pages were configured, return them in sequence.
	if len(f.listPages) > 0 {
		if f.listPageCursor >= len(f.listPages) {
			return &s3.ListObjectsV2Output{}, nil
		}
		page := f.listPages[f.listPageCursor]
		f.listPageCursor++
		return page, nil
	}

	// Default behavior: list every key under the prefix.
	prefix := aws.ToString(in.Prefix)
	out := &s3.ListObjectsV2Output{}
	for k, obj := range f.objects {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		key := k
		lm := obj.lastModified
		out.Contents = append(out.Contents, s3types.Object{
			Key:          &key,
			LastModified: &lm,
		})
	}
	return out, nil
}

func (f *fakeS3) HeadObject(_ context.Context, in *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if f.headError != nil {
		return nil, f.headError
	}
	key := aws.ToString(in.Key)
	if obj, ok := f.headOnly[key]; ok {
		return &s3.HeadObjectOutput{
			Metadata:  obj.metadata,
			VersionId: aws.String(obj.versionID),
		}, nil
	}
	if obj, ok := f.objects[key]; ok {
		return &s3.HeadObjectOutput{
			Metadata:  obj.metadata,
			VersionId: aws.String(obj.versionID),
		}, nil
	}
	return nil, errors.New("NotFound")
}

// newTestStore wires an S3Store backed by a fakeS3.
func newTestStore() (*S3Store, *fakeS3) {
	f := newFakeS3()
	return &S3Store{
		client: f,
		bucket: "test-bucket",
		prefix: "snapshots/",
	}, f
}

// fixtureSnapshot builds a deterministic snapshot for the SaveSnapshot
// / GetLatestSnapshot / GetSnapshot round-trip tests.
func fixtureSnapshot(id string) *types.Snapshot {
	gen := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	return &types.Snapshot{
		SnapshotID:  id,
		Version:     "v2",
		GeneratedAt: gen,
		Summary: types.SnapshotSummary{
			TotalResources:       3,
			GreenCount:           2,
			RedCount:             1,
			CompliancePercentage: 66.67,
		},
		FindingsByType: map[types.ResourceType][]*types.Finding{
			types.ResourceTypeAurora: {
				{ResourceID: "r1", Status: types.StatusGreen},
			},
		},
	}
}

func TestS3Store_SaveSnapshot_WritesBothCanonicalAndLatest(t *testing.T) {
	store, fake := newTestStore()

	snap := fixtureSnapshot("snap-001")
	require.NoError(t, store.SaveSnapshot(context.Background(), snap))

	// Two writes total: timestamped key + latest pointer.
	require.Len(t, fake.objects, 2, "SaveSnapshot must write both the timestamped key and latest.json")

	latest, ok := fake.objects["snapshots/latest.json"]
	require.True(t, ok, "latest.json must be written")
	assert.Equal(t, "snap-001", latest.metadata["snapshot-id"])
	assert.Equal(t, "v2", latest.metadata["schema-version"])

	// Locate the timestamped object.
	var dateKey string
	for k := range fake.objects {
		if k != "snapshots/latest.json" {
			dateKey = k
		}
	}
	require.NotEmpty(t, dateKey)
	assert.Contains(t, dateKey, "2026/04/08", "timestamped key must include YYYY/MM/DD")
	assert.Contains(t, dateKey, "snap-001.json")

	// Body round-trips.
	var out types.Snapshot
	require.NoError(t, json.Unmarshal(latest.body, &out))
	assert.Equal(t, "snap-001", out.SnapshotID)
	assert.Equal(t, "v2", out.Version)
}

func TestS3Store_SaveSnapshot_PutError(t *testing.T) {
	store, fake := newTestStore()
	fake.putError = errors.New("s3 down")

	err := store.SaveSnapshot(context.Background(), fixtureSnapshot("snap-err"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "s3 down")
}

func TestS3Store_GetLatestSnapshot_RoundTrip(t *testing.T) {
	store, _ := newTestStore()

	snap := fixtureSnapshot("snap-latest")
	require.NoError(t, store.SaveSnapshot(context.Background(), snap))

	got, err := store.GetLatestSnapshot(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "snap-latest", got.SnapshotID)
	assert.Equal(t, "v2", got.Version)
	assert.Equal(t, 3, got.Summary.TotalResources)
}

func TestS3Store_GetLatestSnapshot_NotFound(t *testing.T) {
	store, _ := newTestStore()
	_, err := store.GetLatestSnapshot(context.Background())
	require.Error(t, err)
}

func TestS3Store_GetLatestSnapshot_MalformedJSON(t *testing.T) {
	store, fake := newTestStore()
	fake.objects["snapshots/latest.json"] = fakeObject{body: []byte("{not-json}")}

	_, err := store.GetLatestSnapshot(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestS3Store_GetSnapshot_FindsByID(t *testing.T) {
	store, _ := newTestStore()
	require.NoError(t, store.SaveSnapshot(context.Background(), fixtureSnapshot("snap-A")))
	require.NoError(t, store.SaveSnapshot(context.Background(), fixtureSnapshot("snap-B")))

	got, err := store.GetSnapshot(context.Background(), "snap-A")
	require.NoError(t, err)
	assert.Equal(t, "snap-A", got.SnapshotID)
}

func TestS3Store_GetSnapshot_NotFound(t *testing.T) {
	store, _ := newTestStore()
	require.NoError(t, store.SaveSnapshot(context.Background(), fixtureSnapshot("snap-A")))

	_, err := store.GetSnapshot(context.Background(), "missing-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot not found")
}

func TestS3Store_GetSnapshot_PaginatesUntilFound(t *testing.T) {
	store, fake := newTestStore()

	// Pre-canned 2-page response: first page has truncated=true and
	// the wrong key; second page contains the match.
	page1Key := "snapshots/2026/04/08/snap-X.json"
	page2Key := "snapshots/2026/04/08/snap-Y.json"
	tok := "next-page-token"
	truncated := true
	notTruncated := false
	t1 := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 8, 13, 0, 0, 0, time.UTC)

	fake.listPages = []*s3.ListObjectsV2Output{
		{
			Contents:              []s3types.Object{{Key: &page1Key, LastModified: &t1}},
			IsTruncated:           &truncated,
			NextContinuationToken: &tok,
		},
		{
			Contents:    []s3types.Object{{Key: &page2Key, LastModified: &t2}},
			IsTruncated: &notTruncated,
		},
	}

	// And the actual GetObject for the matching page-2 key needs a body.
	body, _ := json.Marshal(fixtureSnapshot("snap-Y"))
	fake.objects[page2Key] = fakeObject{body: body}

	got, err := store.GetSnapshot(context.Background(), "snap-Y")
	require.NoError(t, err)
	assert.Equal(t, "snap-Y", got.SnapshotID)
	assert.Equal(t, 2, fake.listPageCursor, "should have paginated through both pages")
}

func TestS3Store_ListSnapshots_HappyPath(t *testing.T) {
	store, fake := newTestStore()

	require.NoError(t, store.SaveSnapshot(context.Background(), fixtureSnapshot("snap-1")))
	require.NoError(t, store.SaveSnapshot(context.Background(), fixtureSnapshot("snap-2")))

	got, err := store.ListSnapshots(context.Background(), 100)
	require.NoError(t, err)
	// Two real snapshots; latest.json must be skipped.
	assert.Len(t, got, 2)

	for _, m := range got {
		assert.Contains(t, []string{"snap-1", "snap-2"}, m.SnapshotID)
		assert.Equal(t, 3, m.TotalResources, "TotalResources parsed from S3 metadata")
		assert.InDelta(t, 66.67, m.CompliancePercentage, 0.01)
		assert.NotEmpty(t, m.S3Key)
		assert.NotEmpty(t, m.S3VersionID)
	}

	// Confirm the latest pointer was filtered out.
	for _, m := range got {
		assert.NotEqual(t, "snapshots/latest.json", m.S3Key)
	}
	_ = fake
}

func TestS3Store_ListSnapshots_RespectsLimit(t *testing.T) {
	store, _ := newTestStore()

	for i := 0; i < 5; i++ {
		s := fixtureSnapshot("snap-" + string(rune('A'+i)))
		require.NoError(t, store.SaveSnapshot(context.Background(), s))
	}

	got, err := store.ListSnapshots(context.Background(), 3)
	require.NoError(t, err)
	assert.Len(t, got, 3, "ListSnapshots must stop once the caller-supplied limit is met")
}

func TestS3Store_ListSnapshots_ListError(t *testing.T) {
	store, fake := newTestStore()
	fake.listError = errors.New("AccessDenied")

	_, err := store.ListSnapshots(context.Background(), 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AccessDenied")
}

func TestS3Store_ListSnapshots_HeadFailureSkipsObject(t *testing.T) {
	store, fake := newTestStore()

	// One valid object + one that fails HEAD. List should return only
	// the one whose HEAD succeeded — best-effort listing.
	require.NoError(t, store.SaveSnapshot(context.Background(), fixtureSnapshot("good")))
	badKey := "snapshots/2026/04/08/bad.json"
	fake.objects[badKey] = fakeObject{body: []byte("ignored")}
	// Make HEAD fail for everything (including the good one) by toggling
	// headError; then we confirm best-effort returns 0 with no top-level error.
	fake.headError = errors.New("Forbidden")

	got, err := store.ListSnapshots(context.Background(), 100)
	require.NoError(t, err) // top-level call still succeeds
	assert.Empty(t, got)
}

func TestS3Store_GenerateKey_ContainsDateAndID(t *testing.T) {
	store := &S3Store{prefix: "snapshots/"}
	key := store.generateKey(time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC), "abc-123")
	assert.Equal(t, "snapshots/2026/04/08/abc-123.json", key)
}
