package orchestrator

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

// fakeDoer captures the request and returns a canned response or error.
type fakeDoer struct {
	resp    *http.Response
	err     error
	gotURL  string
	gotBody string
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.gotURL = req.URL.String()
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		f.gotBody = string(b)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func makeResp(t *testing.T, status int, body string) *http.Response {
	t.Helper()
	resp := &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
	// The activity-under-test closes resp.Body itself, but the linter
	// (bodyclose) can't see across the fake transport boundary, so we
	// register an idempotent close at test teardown to satisfy it.
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// runNotifyEmitterActivity executes the activity through Temporal's activity
// test environment so activity.GetLogger / activity context plumbing works.
func runNotifyEmitterActivity(t *testing.T, a *Activities, in NotifyEmitterInput) (*NotifyEmitterResult, error) {
	t.Helper()
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivity(a.NotifyEmitter)

	val, err := env.ExecuteActivity(a.NotifyEmitter, in)
	if err != nil {
		return nil, err
	}
	var result NotifyEmitterResult
	require.NoError(t, val.Get(&result))
	return &result, nil
}

func TestNotifyEmitter_Success_ReturnsParsedIDs(t *testing.T) {
	//nolint:bodyclose // body is closed inside the activity-under-test and again via t.Cleanup in makeResp
	doer := &fakeDoer{
		resp: makeResp(t, http.StatusAccepted, `{"workflow_id":"wf-1","run_id":"run-1","snapshot_id":"snap-1"}`),
	}
	a := &Activities{HTTPDoer: doer}

	out, err := runNotifyEmitterActivity(t, a, NotifyEmitterInput{
		EmitterWebhookURL: "http://emitter:8080",
		SnapshotID:        "snap-1",
	})

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "wf-1", out.WorkflowID)
	assert.Equal(t, "run-1", out.RunID)
	assert.Equal(t, "snap-1", out.SnapshotID)
	assert.Equal(t, "http://emitter:8080/trigger-act", doer.gotURL)
	assert.Contains(t, doer.gotBody, `"snapshot_id":"snap-1"`)
}

func TestNotifyEmitter_TrimsTrailingSlash(t *testing.T) {
	//nolint:bodyclose // body is closed inside activity-under-test + t.Cleanup
	doer := &fakeDoer{resp: makeResp(t, http.StatusAccepted, `{}`)}
	a := &Activities{HTTPDoer: doer}

	_, err := runNotifyEmitterActivity(t, a, NotifyEmitterInput{
		EmitterWebhookURL: "http://emitter:8080/",
		SnapshotID:        "snap",
	})

	require.NoError(t, err)
	assert.Equal(t, "http://emitter:8080/trigger-act", doer.gotURL,
		"trailing slash on webhook base must not double-up the path separator")
}

func TestNotifyEmitter_EmptyURL_Errors(t *testing.T) {
	a := &Activities{}
	_, err := runNotifyEmitterActivity(t, a, NotifyEmitterInput{})
	require.Error(t, err)
}

func TestNotifyEmitter_TransportError_Wraps(t *testing.T) {
	doer := &fakeDoer{err: errors.New("connection refused")}
	a := &Activities{HTTPDoer: doer}

	_, err := runNotifyEmitterActivity(t, a, NotifyEmitterInput{
		EmitterWebhookURL: "http://emitter:8080",
		SnapshotID:        "x",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestNotifyEmitter_Non2xxStatus_Errors(t *testing.T) {
	//nolint:bodyclose // body is closed inside activity-under-test + t.Cleanup
	doer := &fakeDoer{resp: makeResp(t, http.StatusInternalServerError, "boom")}
	a := &Activities{HTTPDoer: doer}

	_, err := runNotifyEmitterActivity(t, a, NotifyEmitterInput{
		EmitterWebhookURL: "http://emitter:8080",
		SnapshotID:        "x",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
	assert.Contains(t, err.Error(), "boom")
}

func TestNotifyEmitter_UnparseableBody_StillSucceeds(t *testing.T) {
	//nolint:bodyclose // body is closed inside activity-under-test + t.Cleanup
	doer := &fakeDoer{resp: makeResp(t, http.StatusAccepted, `not json`)}
	a := &Activities{HTTPDoer: doer}

	out, err := runNotifyEmitterActivity(t, a, NotifyEmitterInput{
		EmitterWebhookURL: "http://emitter:8080",
		SnapshotID:        "snap",
	})

	require.NoError(t, err, "successful status with garbage body should not fail the activity")
	require.NotNil(t, out)
	assert.Empty(t, out.WorkflowID, "unparseable body leaves workflow id empty")
}
