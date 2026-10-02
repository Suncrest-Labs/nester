package chaos

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func doGet(t *testing.T, client *http.Client, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	return client.Do(req)
}

func TestFaultNonePassesThrough(t *testing.T) {
	srv := newTestServer(t, `{"ok":true}`)
	client := &http.Client{Transport: New(nil, Script{Fault: FaultNone})}

	resp, err := doGet(t, client, srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"ok":true}`, string(body))
}

func TestFaultTimeoutEndsOnCallerDeadline(t *testing.T) {
	srv := newTestServer(t, `{"ok":true}`)
	client := &http.Client{Transport: New(nil, Script{Fault: FaultTimeout})}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	start := time.Now()
	_, err = client.Do(req)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t, errors.Is(ctx.Err(), context.DeadlineExceeded))
	// Ended on the deadline, not immediately and not indefinitely.
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestFaultLatencyDelaysBeforeForwarding(t *testing.T) {
	srv := newTestServer(t, `{"ok":true}`)
	client := &http.Client{Transport: New(nil, Script{Fault: FaultLatency, Latency: 30 * time.Millisecond})}

	start := time.Now()
	resp, err := doGet(t, client, srv.URL)
	elapsed := time.Since(start)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.GreaterOrEqual(t, elapsed, 30*time.Millisecond)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFaultLatencyPastCallerTimeoutIsAnError(t *testing.T) {
	srv := newTestServer(t, `{"ok":true}`)
	client := &http.Client{
		Transport: New(nil, Script{Fault: FaultLatency, Latency: 200 * time.Millisecond}),
		Timeout:   20 * time.Millisecond,
	}

	_, err := doGet(t, client, srv.URL)
	require.Error(t, err)
}

func TestFaultConnectionResetNeverReachesUpstream(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	client := &http.Client{Transport: New(nil, Script{Fault: FaultConnectionReset})}
	_, err := doGet(t, client, srv.URL)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConnectionReset) || strings.Contains(err.Error(), "connection reset"))
	assert.False(t, called, "the upstream must never be reached when the fault fires before forwarding")
}

func TestFaultRateLimitedWithRetryAfter(t *testing.T) {
	client := &http.Client{Transport: New(nil, Script{Fault: FaultRateLimited, RetryAfter: 5 * time.Second})}

	resp, err := doGet(t, client, "http://example.invalid/")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, "5", resp.Header.Get("Retry-After"))
}

func TestFaultRateLimitedWithoutRetryAfter(t *testing.T) {
	client := &http.Client{Transport: New(nil, Script{Fault: FaultRateLimited})}

	resp, err := doGet(t, client, "http://example.invalid/")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Retry-After"))
}

func TestFaultServerErrorDefaultsTo500(t *testing.T) {
	client := &http.Client{Transport: New(nil, Script{Fault: FaultServerError})}

	resp, err := doGet(t, client, "http://example.invalid/")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestFaultServerErrorCustomStatus(t *testing.T) {
	client := &http.Client{Transport: New(nil, Script{Fault: FaultServerError, ServerErrorStatus: http.StatusBadGateway})}

	resp, err := doGet(t, client, "http://example.invalid/")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
}

func TestFaultMalformedBodyReplacesSuccessfulBody(t *testing.T) {
	srv := newTestServer(t, `{"complete":true,"value":42}`)
	client := &http.Client{Transport: New(nil, Script{Fault: FaultMalformedBody, MalformedBody: []byte(`{"trunc`)})}

	resp, err := doGet(t, client, srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "status is preserved — only the body lies")
	assert.Equal(t, `{"trunc`, string(body))
}

func TestFaultWrongContentTypeLeavesBodyIntact(t *testing.T) {
	srv := newTestServer(t, `{"ok":true}`)
	client := &http.Client{Transport: New(nil, Script{Fault: FaultWrongContentType})}

	resp, err := doGet(t, client, srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
	assert.JSONEq(t, `{"ok":true}`, string(body), "body is untouched — only the header lies")
}

func TestFaultPartialStreamTruncates(t *testing.T) {
	srv := newTestServer(t, `{"this":"is a longer json body than the truncation point"}`)
	client := &http.Client{Transport: New(nil, Script{Fault: FaultPartialStream, PartialStreamBytes: 10})}

	resp, err := doGet(t, client, srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	assert.Len(t, body, 10)
	assert.Equal(t, `{"this":"i`, string(body))
}

func TestFaultSlowDripDeliversOneByteAtATime(t *testing.T) {
	srv := newTestServer(t, "abc")
	client := &http.Client{Transport: New(nil, Script{Fault: FaultSlowDrip, SlowDripInterval: 5 * time.Millisecond})}

	start := time.Now()
	resp, err := doGet(t, client, srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "abc", string(body))
	// 3 bytes at >=5ms apart must take at least 15ms end to end.
	assert.GreaterOrEqual(t, elapsed, 15*time.Millisecond)
}

func TestFaultSlowDripAbortsOnCallerCancel(t *testing.T) {
	srv := newTestServer(t, "abcdefghij")
	client := &http.Client{Transport: New(nil, Script{Fault: FaultSlowDrip, SlowDripInterval: 50 * time.Millisecond})}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err, "the initial response arrives; the drip stalls the body read, not the round trip")
	defer resp.Body.Close()

	_, err = io.ReadAll(resp.Body)
	require.Error(t, err, "reading the slow-dripped body must observe the caller's cancellation")
}

func TestSequenceAppliesFaultsInOrderThenHoldsLast(t *testing.T) {
	transport := New(nil, Script{Sequence: []Fault{FaultServerError, FaultServerError, FaultNone}})
	srv := newTestServer(t, `{"ok":true}`)
	client := &http.Client{Transport: transport}

	statuses := make([]int, 0, 4)
	for i := 0; i < 4; i++ {
		resp, err := doGet(t, client, srv.URL)
		require.NoError(t, err)
		statuses = append(statuses, resp.StatusCode)
		resp.Body.Close()
	}

	assert.Equal(t, []int{500, 500, 200, 200}, statuses, "the 4th call reuses the last Sequence entry (FaultNone)")
	assert.Equal(t, 4, transport.CallCount())
	assert.Equal(t, []Fault{FaultServerError, FaultServerError, FaultNone, FaultNone}, transport.History())
}
