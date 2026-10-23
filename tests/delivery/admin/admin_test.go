package admin_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	metricsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/admin"
	dto "github.com/prometheus/client_model/go"
)

// P19: GET /metrics admin listener'а — 200 + exposition-текст
// (сигналы приложения и go_* коллекторы).
func TestMetricsEndpoint(t *testing.T) {
	t.Parallel()

	m, err := metricsadapter.New(metricsadapter.MetricsConfig{})
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	m.PeerJoined()
	m.SetActiveRooms(2)

	app := admin.NewMetricsApp(m.Gatherer(), stdlib.New("test"))
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain exposition", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "rooms_peer_joins_total 1") {
		t.Fatalf("exposition missing rooms_peer_joins_total 1:\n%s", text)
	}
	if !strings.Contains(text, "rooms_active_rooms 2") {
		t.Fatalf("exposition missing rooms_active_rooms 2:\n%s", text)
	}
	if !strings.Contains(text, "go_goroutines") {
		t.Fatal("exposition missing go collector metrics")
	}
}

// errGatherer - Gatherer, всегда падающий с ошибкой (error-path P19b).
type errGatherer struct{}

func (errGatherer) Gather() ([]*dto.MetricFamily, error) {
	return nil, errors.New("gather boom")
}

// P19b фикс 6: ошибка Gather не глотается - handler логирует и отдаёт 500
// с контекстом (обёрнутая ошибка, не голый ErrInternalServerError).
func TestMetricsEndpoint_GatherError(t *testing.T) {
	t.Parallel()

	app := admin.NewMetricsApp(errGatherer{}, stdlib.New("test"))
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "gather") {
		t.Fatalf("error body missing gather context: %q", body)
	}
}
