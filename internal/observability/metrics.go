package observability

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}

type Metrics struct {
	enabled    bool
	mu         sync.RWMutex
	counters   map[string]*metricCounter
	histograms map[string]*metricHistogram
	gauges     map[string]*metricGauge
}

type metricCounter struct {
	name   string
	help   string
	labels map[string]string
	value  float64
}

type metricHistogram struct {
	name    string
	help    string
	labels  map[string]string
	buckets []uint64
	count   uint64
	sum     float64
}

type metricGauge struct {
	name   string
	help   string
	labels map[string]string
	value  float64
}

func NewMetrics(enabled bool) *Metrics {
	return &Metrics{enabled: enabled, counters: map[string]*metricCounter{}, histograms: map[string]*metricHistogram{}, gauges: map[string]*metricGauge{}}
}

func (m *Metrics) Enabled() bool { return m != nil && m.enabled }

func (m *Metrics) HTTP(method, route string, statusCode int, duration time.Duration) {
	statusClass := fmt.Sprintf("%dxx", statusCode/100)
	labels := map[string]string{"method": bounded(method, "OTHER", 12), "route": boundedRoute(route), "status_class": statusClass}
	m.add("forgeflow_http_requests_total", "HTTP requests handled.", labels, 1)
	m.observe("forgeflow_http_request_duration_seconds", "HTTP request duration in seconds.", labels, duration.Seconds())
}

func (m *Metrics) RunTerminal(status string) {
	m.Run(status, false, false, false, 0)
}
func (m *Metrics) Run(status string, firstPass, recovered, budgetExhausted bool, repairs int) {
	status = enum(status, "completed", "failed", "cancelled")
	m.add("forgeflow_runs_terminal_total", "Runs entering a terminal state.", map[string]string{"status": status}, 1)
	if status == "completed" {
		m.add("forgeflow_runs_first_pass_total", "Completed runs by first-pass result.", map[string]string{"first_pass": strconv.FormatBool(firstPass)}, 1)
	}
	if recovered {
		m.add("forgeflow_runs_recovery_total", "Resumed runs by terminal outcome.", map[string]string{"status": status}, 1)
	}
	if budgetExhausted {
		m.add("forgeflow_budget_exhaustions_total", "Runs stopped by budget enforcement.", nil, 1)
	}
	if repairs > 0 {
		m.add("forgeflow_repairs_total", "Repair iterations executed.", nil, float64(repairs))
	}
}
func (m *Metrics) Node(node, status string, duration time.Duration) {
	labels := map[string]string{"node": bounded(node, "unknown", 64), "status": enum(status, "completed", "interrupted", "failed", "cancelled")}
	m.add("forgeflow_graph_node_attempts_total", "Graph node attempts.", labels, 1)
	m.observe("forgeflow_graph_node_duration_seconds", "Graph node duration in seconds.", labels, duration.Seconds())
}
func (m *Metrics) Model(provider, modelName, status string, input, output, cached int, cost float64, duration time.Duration) {
	labels := map[string]string{"provider": bounded(provider, "unknown", 32), "model": bounded(modelName, "unknown", 64), "status": bounded(status, "unknown", 24)}
	m.add("forgeflow_model_requests_total", "Model requests.", labels, 1)
	m.observe("forgeflow_model_request_duration_seconds", "Model request duration in seconds.", labels, duration.Seconds())
	for kind, value := range map[string]int{"input": input, "output": output, "cached_input": cached} {
		m.add("forgeflow_model_tokens_total", "Model tokens by kind.", map[string]string{"provider": labels["provider"], "model": labels["model"], "kind": kind}, float64(max(value, 0)))
	}
	m.add("forgeflow_model_estimated_cost_usd_total", "Estimated model cost in USD.", map[string]string{"provider": labels["provider"], "model": labels["model"]}, max(cost, 0))
}
func (m *Metrics) Tool(name, status, action string, duration time.Duration) {
	labels := map[string]string{"tool": bounded(name, "unknown", 64), "status": bounded(status, "unknown", 32), "policy_action": enum(action, "allow", "deny", "require_approval")}
	m.add("forgeflow_tool_calls_total", "Tool calls.", labels, 1)
	m.observe("forgeflow_tool_call_duration_seconds", "Tool call duration in seconds.", labels, duration.Seconds())
}
func (m *Metrics) Approval(decision string, wait time.Duration) {
	labels := map[string]string{"decision": enum(decision, "approved", "rejected")}
	m.add("forgeflow_approval_decisions_total", "Approval decisions.", labels, 1)
	m.observe("forgeflow_approval_wait_duration_seconds", "Approval wait duration in seconds.", labels, wait.Seconds())
}
func (m *Metrics) Queue(event string) {
	m.add("forgeflow_queue_events_total", "Worker queue lifecycle events.", map[string]string{"event": enum(event, "leased", "completed", "failed", "lease_lost", "empty")}, 1)
}
func (m *Metrics) QueueDepth(depth int) {
	m.set("forgeflow_queue_depth", "Jobs waiting or eligible for lease.", nil, float64(max(depth, 0)))
}
func (m *Metrics) Auth(outcome string) {
	m.add("forgeflow_auth_attempts_total", "Authentication attempts.", map[string]string{"outcome": enum(outcome, "success", "failure", "rate_limited")}, 1)
}
func (m *Metrics) RateLimited(scope string) {
	m.add("forgeflow_rate_limit_rejections_total", "Requests rejected by rate limiting.", map[string]string{"scope": enum(scope, "api", "login")}, 1)
}

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !m.Enabled() {
			http.NotFound(w, nil)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, m.Prometheus())
	})
}

func (m *Metrics) Prometheus() string {
	if !m.Enabled() {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var output strings.Builder
	counterKeys := sortedKeys(m.counters)
	last := ""
	for _, key := range counterKeys {
		item := m.counters[key]
		if item.name != last {
			fmt.Fprintf(&output, "# HELP %s %s\n# TYPE %s counter\n", item.name, item.help, item.name)
			last = item.name
		}
		fmt.Fprintf(&output, "%s%s %s\n", item.name, formatLabels(item.labels, nil), strconv.FormatFloat(item.value, 'f', -1, 64))
	}
	histogramKeys := sortedKeys(m.histograms)
	last = ""
	for _, key := range histogramKeys {
		item := m.histograms[key]
		if item.name != last {
			fmt.Fprintf(&output, "# HELP %s %s\n# TYPE %s histogram\n", item.name, item.help, item.name)
			last = item.name
		}
		for index, limit := range durationBuckets {
			fmt.Fprintf(&output, "%s_bucket%s %d\n", item.name, formatLabels(item.labels, map[string]string{"le": strconv.FormatFloat(limit, 'f', -1, 64)}), item.buckets[index])
		}
		fmt.Fprintf(&output, "%s_bucket%s %d\n", item.name, formatLabels(item.labels, map[string]string{"le": "+Inf"}), item.count)
		fmt.Fprintf(&output, "%s_sum%s %s\n%s_count%s %d\n", item.name, formatLabels(item.labels, nil), strconv.FormatFloat(item.sum, 'f', -1, 64), item.name, formatLabels(item.labels, nil), item.count)
	}
	gaugeKeys := sortedKeys(m.gauges)
	last = ""
	for _, key := range gaugeKeys {
		item := m.gauges[key]
		if item.name != last {
			fmt.Fprintf(&output, "# HELP %s %s\n# TYPE %s gauge\n", item.name, item.help, item.name)
			last = item.name
		}
		fmt.Fprintf(&output, "%s%s %s\n", item.name, formatLabels(item.labels, nil), strconv.FormatFloat(item.value, 'f', -1, 64))
	}
	return output.String()
}

func (m *Metrics) add(name, help string, labels map[string]string, value float64) {
	if !m.Enabled() || value == 0 {
		return
	}
	key := name + formatLabels(labels, nil)
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.counters[key]
	if item == nil {
		item = &metricCounter{name: name, help: help, labels: cloneLabels(labels)}
		m.counters[key] = item
	}
	item.value += value
}
func (m *Metrics) observe(name, help string, labels map[string]string, value float64) {
	if !m.Enabled() || value < 0 {
		return
	}
	key := name + formatLabels(labels, nil)
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.histograms[key]
	if item == nil {
		item = &metricHistogram{name: name, help: help, labels: cloneLabels(labels), buckets: make([]uint64, len(durationBuckets))}
		m.histograms[key] = item
	}
	item.count++
	item.sum += value
	for index, limit := range durationBuckets {
		if value <= limit {
			item.buckets[index]++
		}
	}
}
func (m *Metrics) set(name, help string, labels map[string]string, value float64) {
	if !m.Enabled() {
		return
	}
	key := name + formatLabels(labels, nil)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gauges[key] = &metricGauge{name: name, help: help, labels: cloneLabels(labels), value: value}
}
func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func cloneLabels(labels map[string]string) map[string]string {
	result := make(map[string]string, len(labels))
	for key, value := range labels {
		result[key] = value
	}
	return result
}
func formatLabels(labels, extra map[string]string) string {
	if len(labels)+len(extra) == 0 {
		return ""
	}
	merged := cloneLabels(labels)
	for key, value := range extra {
		merged[key] = value
	}
	keys := sortedKeys(merged)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"=\""+escapeLabel(merged[key])+"\"")
	}
	return "{" + strings.Join(parts, ",") + "}"
}
func escapeLabel(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"").Replace(value)
}
func bounded(value, fallback string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit {
		return fallback
	}
	return value
}
func boundedRoute(route string) string {
	route = bounded(route, "unmatched", 160)
	if strings.ContainsAny(route, "?#\r\n") {
		return "unmatched"
	}
	return route
}
func enum(value string, allowed ...string) string {
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return "other"
}
