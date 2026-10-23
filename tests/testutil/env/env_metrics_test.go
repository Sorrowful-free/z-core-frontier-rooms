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

// P19b пункт 12: env.WithMetrics - реальный prometheus-адаптер во всех
// метрико-ёмких компонентах + admin app с GET /metrics на том же
// fiber.Test-клиенте; e2e smoke exposition + сигнал HTTP middleware.
func TestEnvWithMetrics_AdminMetricsEndpoint(t *testing.T) {
	t.Parallel()

	e := env.New(t, env.WithMetrics())
	if e.Metrics == nil {
		t.Fatal("WithMetrics: Env.Metrics not wired")
	}
	if e.Admin == nil {
		t.Fatal("WithMetrics: Env.Admin not wired")
	}

	// Запрос к публичному app: metrics middleware обязан наблюдать его
	// по route-шаблону, а не raw path.
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

// Без WithMetrics поведение env не меняется: поля телеметрии пусты.
func TestEnvWithoutMetrics_NoTelemetry(t *testing.T) {
	t.Parallel()

	e := env.New(t)
	if e.Metrics != nil || e.Admin != nil {
		t.Fatal("without WithMetrics: Metrics/Admin must stay nil")
	}
}
