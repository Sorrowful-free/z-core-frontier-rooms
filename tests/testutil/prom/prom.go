// Package prom - asserts on top of prometheus.Gatherer for metrics tests:
// reading a metric value by family name and a subset of labels.
// Consolidates per-test copies of the gather loop (P19b).
package prom

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Value returns the value of a metric from family name whose labels cover
// wantLabels (gauge/counter - the value; summary/histogram - sample count,
// i.e. the number of observations). t.Fatalf if the metric is not found.
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
