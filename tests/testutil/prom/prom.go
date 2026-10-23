// Package prom - asserts поверх prometheus.Gatherer для метрико-тестов:
// чтение значения метрики по имени семейства и подмножеству label'ов.
// Консолидирует per-test копии gather-цикла (P19b).
package prom

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Value возвращает значение метрики семейства name, чьи label'ы покрывают
// wantLabels (gauge/counter - значение; summary/histogram - sample count,
// т.е. число наблюдений). t.Fatalf, если метрика не найдена.
func Value(t *testing.T, g prometheus.Gatherer, name string, wantLabels map[string]string) float64 {
	t.Helper()
	families, err := g.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() != name {
			continue
		}
		for _, metric := range mf.GetMetric() {
			if labelsMatch(metric, wantLabels) {
				return metricValue(metric)
			}
		}
	}
	t.Fatalf("metric %q with labels %v not found", name, wantLabels)
	return 0
}

func metricValue(m *dto.Metric) float64 {
	if g := m.GetGauge(); g != nil {
		return g.GetValue()
	}
	if c := m.GetCounter(); c != nil {
		return c.GetValue()
	}
	if s := m.GetSummary(); s != nil {
		return float64(s.GetSampleCount())
	}
	if h := m.GetHistogram(); h != nil {
		return float64(h.GetSampleCount())
	}
	return 0
}

func labelsMatch(metric *dto.Metric, want map[string]string) bool {
	got := make(map[string]string, len(metric.GetLabel()))
	for _, lp := range metric.GetLabel() {
		got[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}
