package metric

import (
	"time"
)

type Metric struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Unit        string            `json:"unit,omitempty"`
	Type        MetricType        `json:"type"`
	Value       float64           `json:"value"`
	Labels      map[string]string `json:"labels,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
	Resource    map[string]string `json:"resource,omitempty"`
}

type MetricType string

const (
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeCounter   MetricType = "counter"
	MetricTypeHistogram MetricType = "histogram"
	MetricTypeSummary   MetricType = "summary"
)

type MetricDataPoint struct {
	Timestamp time.Time         `json:"timestamp"`
	Value     float64           `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
	Count     uint64            `json:"count,omitempty"`
	Sum       float64           `json:"sum,omitempty"`
	Buckets   []BucketCount     `json:"buckets,omitempty"`
	Quantiles []QuantileValue   `json:"quantiles,omitempty"`
}

type BucketCount struct {
	UpperBound float64 `json:"upperBound"`
	Count      uint64  `json:"count"`
}

type QuantileValue struct {
	Quantile float64 `json:"quantile"`
	Value    float64 `json:"value"`
}

type MetricSeries struct {
	Name       string            `json:"name"`
	Type       MetricType        `json:"type"`
	Labels     map[string]string `json:"labels,omitempty"`
	DataPoints []MetricDataPoint `json:"dataPoints"`
}

type QueryResult struct {
	Series   []MetricSeries `json:"series"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

type Alert struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Severity    AlertSeverity     `json:"severity"`
	Metric      string            `json:"metric,omitempty"`
	Condition   string            `json:"condition"`
	Threshold   float64           `json:"threshold"`
	Duration    time.Duration     `json:"duration"`
	State       AlertState        `json:"state"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	FiredAt     *time.Time        `json:"firedAt,omitempty"`
	ResolvedAt  *time.Time        `json:"resolvedAt,omitempty"`
}

type AlertSeverity string

const (
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityInfo     AlertSeverity = "info"
)

type AlertState string

const (
	AlertStateInactive AlertState = "inactive"
	AlertStatePending  AlertState = "pending"
	AlertStateFiring   AlertState = "firing"
	AlertStateResolved AlertState = "resolved"
)

type Dashboard struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Panels      []DashboardPanel `json:"panels"`
	Variables   []Variable       `json:"variables,omitempty"`
	Refresh     time.Duration    `json:"refresh,omitempty"`
	TimeRange   TimeRange        `json:"timeRange"`
}

type DashboardPanel struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Type    PanelType     `json:"type"`
	Targets []QueryTarget `json:"targets"`
	X       int           `json:"x,omitempty"`
	Y       int           `json:"y,omitempty"`
	Width   int           `json:"width,omitempty"`
	Height  int           `json:"height,omitempty"`
}

type PanelType string

const (
	PanelTypeGraph PanelType = "graph"
	PanelTypeStat  PanelType = "stat"
	PanelTypeGauge PanelType = "gauge"
	PanelTypeTable PanelType = "table"
	PanelTypeLog   PanelType = "log"
	PanelTypeTrace PanelType = "trace"
)

type QueryTarget struct {
	Expr   string            `json:"expr"`
	Legend string            `json:"legend,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

type Variable struct {
	Name   string       `json:"name"`
	Type   VariableType `json:"type"`
	Query  string       `json:"query,omitempty"`
	Values []string     `json:"values,omitempty"`
}

type VariableType string

const (
	VariableTypeQuery  VariableType = "query"
	VariableTypeConst  VariableType = "constant"
	VariableTypeCustom VariableType = "custom"
)

type TimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}
