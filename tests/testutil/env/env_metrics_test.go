package env_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/prom"
)

// P19b item 12: env.WithMetrics - a real prometheus adapter in all
// metrics-bearing components + an admin app with GET /metrics on the same
// fiber.Test client; e2e smoke of exposition + the HTTP middleware signal.
func TestEnvWithMetrics_AdminMetricsEndpoint(t *testing.T) {
	t.Parallel()

	e := env.New(t, env.WithMetrics())
	if e.Metrics == nil {
		t.Fatal("WithMetrics: Env.Metrics not wired")
	}
	if e.Admin == nil {
		t.Fatal("WithMetrics: Env.Admin not wired")
	}

	// Request to the public app: the metrics middleware must observe it
	// by route template, not by raw path.
	resp, _ := e.Do(t, http.MethodGet, "/rooms", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /rooms status = %d, want 200", resp.StatusCode)
	}
	if got := prom.Value(t, e.Metrics.Gatherer(), "rooms_http_request_duration_seconds",
		map[string]string{"route": "/rooms", "method": "GET", "status": "200"}); got < 1 {
		t.Fatalf("http_request_duration{route=/rooms} count = %v, want >= 1", got)
	}

	mresp, err := e.Admin.Test(httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if err != nil {
		t.Fatalf("admin app.Test: %v", err)
	}
	defer mresp.Body.Close()

	if mresp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics status = %d, want 200", mresp.StatusCode)
	}
	body, err := io.ReadAll(mresp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "rooms_http_request_duration_seconds") {
		t.Fatal("exposition missing rooms_* application metrics")
	}
}

// Without WithMetrics the env behavior is unchanged: telemetry fields are empty.
func TestEnvWithoutMetrics_NoTelemetry(t *testing.T) {
	t.Parallel()

	e := env.New(t)
	if e.Metrics != nil || e.Admin != nil {
		t.Fatal("without WithMetrics: Metrics/Admin must stay nil")
	}
}
