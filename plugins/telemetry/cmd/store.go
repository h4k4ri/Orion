package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	orionmetric "github.com/horizon/orion/plugins/telemetry/metric"
	promclient "github.com/prometheus/client_golang/prometheus"
)

type metricState struct {
	definition *orionmetric.Metric
	series     map[string]*orionmetric.MetricSeries
	promName   string
}

type telemetryStore struct {
	mu           sync.RWMutex
	stateFile    string
	metrics      map[string]*metricState
	alerts       map[string]*orionmetric.Alert
	dashboards   map[string]orionmetric.Dashboard
	pendingSince map[string]time.Time
}

func newTelemetryStore() *telemetryStore {
	return newTelemetryStoreWithFile("")
}

func newTelemetryStoreWithFile(stateFile string) *telemetryStore {
	return &telemetryStore{
		stateFile:    stateFile,
		metrics:      make(map[string]*metricState),
		alerts:       make(map[string]*orionmetric.Alert),
		dashboards:   make(map[string]orionmetric.Dashboard),
		pendingSince: make(map[string]time.Time),
	}
}

type persistedTelemetry struct {
	Metrics      []*persistedMetric      `json:"metrics"`
	Alerts       []*orionmetric.Alert    `json:"alerts"`
	Dashboards   []orionmetric.Dashboard `json:"dashboards"`
	PendingSince map[string]time.Time    `json:"pending_since"`
}

type persistedMetric struct {
	Definition *orionmetric.Metric                  `json:"definition"`
	Series     map[string]*orionmetric.MetricSeries `json:"series"`
	PromName   string                               `json:"prom_name"`
}

// load restores the durable telemetry snapshot. A missing file is treated as
// a fresh installation; malformed state is rejected so operators do not run
// with silently lost metrics and alert state.
func (s *telemetryStore) load() error {
	if strings.TrimSpace(s.stateFile) == "" {
		return nil
	}
	payload, err := os.ReadFile(s.stateFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var state persistedTelemetry
	if err := json.Unmarshal(payload, &state); err != nil {
		return fmt.Errorf("decode telemetry state: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range state.Metrics {
		if item == nil || item.Definition == nil || item.Definition.Name == "" {
			continue
		}
		if item.Series == nil {
			item.Series = make(map[string]*orionmetric.MetricSeries)
		}
		if item.PromName == "" {
			item.PromName = sanitizeMetricName(item.Definition.Name)
		}
		s.metrics[item.Definition.Name] = &metricState{
			definition: cloneMetric(item.Definition),
			series:     item.Series,
			promName:   item.PromName,
		}
	}
	for _, alert := range state.Alerts {
		if alert != nil && alert.ID != "" {
			s.alerts[alert.ID] = cloneAlert(alert)
		}
	}
	for _, dashboard := range state.Dashboards {
		if dashboard.ID != "" {
			s.dashboards[dashboard.ID] = dashboard
		}
	}
	for id, at := range state.PendingSince {
		s.pendingSince[id] = at
	}
	return nil
}

func (s *telemetryStore) persistLocked() error {
	if strings.TrimSpace(s.stateFile) == "" {
		return nil
	}
	state := persistedTelemetry{
		Metrics:      make([]*persistedMetric, 0, len(s.metrics)),
		Alerts:       make([]*orionmetric.Alert, 0, len(s.alerts)),
		Dashboards:   make([]orionmetric.Dashboard, 0, len(s.dashboards)),
		PendingSince: make(map[string]time.Time, len(s.pendingSince)),
	}
	for _, metric := range s.metrics {
		series := make(map[string]*orionmetric.MetricSeries, len(metric.series))
		for key, item := range metric.series {
			copySeries := *item
			copySeries.Labels = cloneLabels(item.Labels)
			copySeries.DataPoints = append([]orionmetric.MetricDataPoint(nil), item.DataPoints...)
			for index := range copySeries.DataPoints {
				copySeries.DataPoints[index].Labels = cloneLabels(copySeries.DataPoints[index].Labels)
			}
			series[key] = &copySeries
		}
		state.Metrics = append(state.Metrics, &persistedMetric{
			Definition: cloneMetric(metric.definition),
			Series:     series,
			PromName:   metric.promName,
		})
	}
	for _, alert := range s.alerts {
		state.Alerts = append(state.Alerts, cloneAlert(alert))
	}
	for _, dashboard := range s.dashboards {
		state.Dashboards = append(state.Dashboards, dashboard)
	}
	for id, at := range s.pendingSince {
		state.PendingSince[id] = at
	}
	sort.Slice(state.Metrics, func(i, j int) bool { return state.Metrics[i].Definition.Name < state.Metrics[j].Definition.Name })
	sort.Slice(state.Alerts, func(i, j int) bool { return state.Alerts[i].ID < state.Alerts[j].ID })
	sort.Slice(state.Dashboards, func(i, j int) bool { return state.Dashboards[i].ID < state.Dashboards[j].ID })
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.stateFile), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.stateFile), ".telemetry-state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.stateFile)
}

func (s *telemetryStore) createMetric(m *orionmetric.Metric) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.metrics[m.Name]; exists {
		return fmt.Errorf("metric already exists: %s", m.Name)
	}
	s.metrics[m.Name] = &metricState{
		definition: cloneMetric(m),
		series:     make(map[string]*orionmetric.MetricSeries),
		promName:   sanitizeMetricName(m.Name),
	}
	return s.persistLocked()
}

func (s *telemetryStore) getMetric(name string) (*orionmetric.Metric, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.metrics[name]
	if !ok {
		return nil, false
	}
	return cloneMetric(m.definition), true
}

func (s *telemetryStore) listMetrics() []*orionmetric.Metric {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*orionmetric.Metric, 0, len(s.metrics))
	for _, state := range s.metrics {
		items = append(items, cloneMetric(state.definition))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func (s *telemetryStore) record(name string, value float64, labels map[string]string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.metrics[name]
	if !ok {
		return fmt.Errorf("metric not found: %s", name)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	merged := make(map[string]string, len(state.definition.Labels)+len(labels))
	for key, item := range state.definition.Labels {
		merged[key] = item
	}
	for key, item := range labels {
		merged[key] = item
	}
	key := labelsKey(merged)
	series, exists := state.series[key]
	if !exists {
		series = &orionmetric.MetricSeries{Name: name, Type: state.definition.Type, Labels: cloneLabels(merged)}
		state.series[key] = series
	}
	series.DataPoints = append(series.DataPoints, orionmetric.MetricDataPoint{
		Timestamp: at,
		Value:     value,
		Labels:    cloneLabels(merged),
	})
	if len(series.DataPoints) > 10000 {
		series.DataPoints = series.DataPoints[len(series.DataPoints)-10000:]
	}
	state.definition.Value = value
	state.definition.Timestamp = at
	s.evaluateAlertsLocked(name, value, at)
	return s.persistLocked()
}

func (s *telemetryStore) query(expr string, start, end time.Time) []orionmetric.MetricSeries {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]orionmetric.MetricSeries, 0)
	for name, state := range s.metrics {
		if expr != "" && expr != name && expr != state.promName {
			continue
		}
		for _, series := range state.series {
			copySeries := *series
			copySeries.Labels = cloneLabels(series.Labels)
			copySeries.DataPoints = make([]orionmetric.MetricDataPoint, 0, len(series.DataPoints))
			for _, point := range series.DataPoints {
				if !start.IsZero() && point.Timestamp.Before(start) {
					continue
				}
				if !end.IsZero() && point.Timestamp.After(end) {
					continue
				}
				point.Labels = cloneLabels(point.Labels)
				copySeries.DataPoints = append(copySeries.DataPoints, point)
			}
			if len(copySeries.DataPoints) > 0 {
				result = append(result, copySeries)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (s *telemetryStore) createAlert(alert *orionmetric.Alert) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.alerts[alert.ID]; exists {
		return fmt.Errorf("alert already exists: %s", alert.ID)
	}
	s.alerts[alert.ID] = cloneAlert(alert)
	return s.persistLocked()
}

func (s *telemetryStore) getAlert(id string) (*orionmetric.Alert, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.alerts[id]
	if !ok {
		return nil, false
	}
	return cloneAlert(a), true
}

func (s *telemetryStore) listAlerts() []*orionmetric.Alert {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*orionmetric.Alert, 0, len(s.alerts))
	for _, alert := range s.alerts {
		items = append(items, cloneAlert(alert))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (s *telemetryStore) evaluateAlertsLocked(metric string, value float64, at time.Time) {
	for id, alert := range s.alerts {
		if alert.Metric != metric {
			continue
		}
		breached := conditionMatches(alert.Condition, value, alert.Threshold)
		if !breached {
			if alert.State == orionmetric.AlertStatePending || alert.State == orionmetric.AlertStateFiring {
				alert.State = orionmetric.AlertStateResolved
				resolved := at
				alert.ResolvedAt = &resolved
			}
			delete(s.pendingSince, id)
			continue
		}
		if alert.State == orionmetric.AlertStateFiring {
			continue
		}
		pending, exists := s.pendingSince[id]
		if !exists {
			pending = at
			s.pendingSince[id] = pending
			alert.State = orionmetric.AlertStatePending
		}
		if alert.Duration <= 0 || at.Sub(pending) >= alert.Duration {
			fired := at
			alert.State = orionmetric.AlertStateFiring
			alert.FiredAt = &fired
		}
	}
}

func (s *telemetryStore) saveDashboard(dashboard orionmetric.Dashboard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dashboards[dashboard.ID] = dashboard
	_ = s.persistLocked()
}

func (s *telemetryStore) getDashboard(id string) (orionmetric.Dashboard, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dashboard, ok := s.dashboards[id]
	return dashboard, ok
}

type metricCollector struct{ store *telemetryStore }

func (c *metricCollector) Describe(_ chan<- *promclient.Desc) {}

func (c *metricCollector) Collect(ch chan<- promclient.Metric) {
	c.store.mu.RLock()
	defer c.store.mu.RUnlock()
	for _, state := range c.store.metrics {
		labelNames := sortedLabelNames(state.definition.Labels)
		for _, series := range state.series {
			values := make([]string, 0, len(labelNames))
			for _, name := range labelNames {
				values = append(values, series.Labels[name])
			}
			value := float64(0)
			if count := len(series.DataPoints); count > 0 {
				value = series.DataPoints[count-1].Value
			}
			metricType := promclient.GaugeValue
			if state.definition.Type == orionmetric.MetricTypeCounter {
				metricType = promclient.CounterValue
			}
			ch <- promclient.MustNewConstMetric(
				promclient.NewDesc(state.promName, state.definition.Description, labelNames, nil),
				metricType, value, values...,
			)
		}
	}
}

var metricNamePattern = regexp.MustCompile(`[^a-zA-Z0-9_:]`)

func sanitizeMetricName(name string) string {
	name = metricNamePattern.ReplaceAllString(name, "_")
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		name = "metric_" + name
	}
	return "orion_" + name
}

func labelsKey(labels map[string]string) string {
	names := sortedLabelNames(labels)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+labels[name])
	}
	return strings.Join(parts, "\x00")
}

func sortedLabelNames(labels map[string]string) []string {
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func conditionMatches(condition string, value, threshold float64) bool {
	condition = strings.ToLower(strings.TrimSpace(condition))
	switch condition {
	case "above", "greater_than", "gt", ">":
		return value > threshold
	case "at_or_above", "gte", ">=":
		return value >= threshold
	case "below", "less_than", "lt", "<":
		return value < threshold
	case "at_or_below", "lte", "<=":
		return value <= threshold
	case "equal", "eq", "=", "==":
		return value == threshold
	default:
		return value > threshold
	}
}

func cloneMetric(m *orionmetric.Metric) *orionmetric.Metric {
	copy := *m
	copy.Labels = cloneLabels(m.Labels)
	copy.Resource = cloneLabels(m.Resource)
	return &copy
}

func cloneAlert(a *orionmetric.Alert) *orionmetric.Alert {
	copy := *a
	copy.Labels = cloneLabels(a.Labels)
	copy.Annotations = cloneLabels(a.Annotations)
	return &copy
}

func cloneLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}
	copy := make(map[string]string, len(labels))
	for key, value := range labels {
		copy[key] = value
	}
	return copy
}
