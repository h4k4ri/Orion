package main

import (
	"path/filepath"
	"testing"
	"time"

	orionmetric "github.com/horizon/orion/plugins/telemetry/metric"
)

func TestTelemetryStoreRecordsAndQueriesSeries(t *testing.T) {
	store := newTelemetryStore()
	if err := store.createMetric(&orionmetric.Metric{Name: "cpu_usage", Type: orionmetric.MetricTypeGauge}); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(100, 0).UTC()
	if err := store.record("cpu_usage", 72.5, map[string]string{"host": "node-a"}, at); err != nil {
		t.Fatal(err)
	}
	series := store.query("cpu_usage", time.Unix(99, 0), time.Unix(101, 0))
	if len(series) != 1 || len(series[0].DataPoints) != 1 || series[0].DataPoints[0].Value != 72.5 {
		t.Fatalf("unexpected query result: %#v", series)
	}
}

func TestTelemetryStoreTransitionsAlert(t *testing.T) {
	store := newTelemetryStore()
	if err := store.createMetric(&orionmetric.Metric{Name: "queue_depth", Type: orionmetric.MetricTypeGauge}); err != nil {
		t.Fatal(err)
	}
	alert := &orionmetric.Alert{ID: "alert-1", Metric: "queue_depth", Condition: "above", Threshold: 10, Duration: time.Second, State: orionmetric.AlertStateInactive}
	if err := store.createAlert(alert); err != nil {
		t.Fatal(err)
	}
	if err := store.record("queue_depth", 11, nil, time.Unix(100, 0)); err != nil {
		t.Fatal(err)
	}
	current, _ := store.getAlert("alert-1")
	if current.State != orionmetric.AlertStatePending {
		t.Fatalf("expected pending alert, got %s", current.State)
	}
	if err := store.record("queue_depth", 12, nil, time.Unix(102, 0)); err != nil {
		t.Fatal(err)
	}
	current, _ = store.getAlert("alert-1")
	if current.State != orionmetric.AlertStateFiring || current.FiredAt == nil {
		t.Fatalf("expected firing alert, got %#v", current)
	}
	if err := store.record("queue_depth", 1, nil, time.Unix(103, 0)); err != nil {
		t.Fatal(err)
	}
	current, _ = store.getAlert("alert-1")
	if current.State != orionmetric.AlertStateResolved || current.ResolvedAt == nil {
		t.Fatalf("expected resolved alert, got %#v", current)
	}
}

func TestTelemetryStorePersistsDashboard(t *testing.T) {
	store := newTelemetryStore()
	dashboard := orionmetric.Dashboard{ID: "dash-1", Name: "Operations"}
	store.saveDashboard(dashboard)
	loaded, ok := store.getDashboard(dashboard.ID)
	if !ok || loaded.Name != dashboard.Name {
		t.Fatalf("dashboard was not stored: %#v", loaded)
	}
}

func TestTelemetryStorePersistsAndLoadsState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.json")
	store := newTelemetryStoreWithFile(path)
	metric := &orionmetric.Metric{Name: "requests", Type: orionmetric.MetricTypeCounter}
	if err := store.createMetric(metric); err != nil {
		t.Fatal(err)
	}
	if err := store.record("requests", 4, map[string]string{"service": "api"}, time.Unix(200, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	alert := &orionmetric.Alert{ID: "alert-persisted", Metric: "requests", Condition: "above", Threshold: 3, State: orionmetric.AlertStateInactive}
	if err := store.createAlert(alert); err != nil {
		t.Fatal(err)
	}
	store.saveDashboard(orionmetric.Dashboard{ID: "dash-persisted", Name: "Operations"})

	loaded := newTelemetryStoreWithFile(path)
	if err := loaded.load(); err != nil {
		t.Fatal(err)
	}
	series := loaded.query("requests", time.Time{}, time.Time{})
	if len(series) != 1 || len(series[0].DataPoints) != 1 || series[0].DataPoints[0].Value != 4 {
		t.Fatalf("unexpected persisted series: %#v", series)
	}
	if current, ok := loaded.getAlert("alert-persisted"); !ok || current.Metric != "requests" {
		t.Fatalf("alert was not persisted: %#v", current)
	}
	if dashboard, ok := loaded.getDashboard("dash-persisted"); !ok || dashboard.Name != "Operations" {
		t.Fatalf("dashboard was not persisted: %#v", dashboard)
	}
}
