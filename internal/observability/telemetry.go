package observability

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const instrumentationName = "forgeflow"

type Options struct {
	ServiceName  string
	Version      string
	Environment  string
	OTLPEndpoint string
	SampleRatio  float64
	Metrics      bool
}

type Telemetry struct {
	provider *sdktrace.TracerProvider
	metrics  *Metrics
	once     sync.Once
}

var (
	defaultMu      sync.RWMutex
	defaultMetrics = NewMetrics(false)
)

func NewTelemetry(ctx context.Context, options Options) (*Telemetry, error) {
	if strings.TrimSpace(options.ServiceName) == "" {
		return nil, fmt.Errorf("telemetry service name is required")
	}
	if options.SampleRatio < 0 || options.SampleRatio > 1 {
		return nil, fmt.Errorf("telemetry sample ratio must be between 0 and 1")
	}
	metrics := NewMetrics(options.Metrics)
	telemetry := &Telemetry{metrics: metrics}
	endpoint := strings.TrimSpace(options.OTLPEndpoint)
	if endpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		setDefaultMetrics(metrics)
		return telemetry, nil
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	res := resource.NewSchemaless(
		attribute.String("service.name", options.ServiceName),
		attribute.String("service.version", emptyAs(options.Version, "development")),
		attribute.String("deployment.environment.name", emptyAs(options.Environment, "development")),
	)
	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(options.SampleRatio))
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res), sdktrace.WithSampler(sampler))
	telemetry.provider = provider
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	setDefaultMetrics(metrics)
	return telemetry, nil
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	var shutdownErr error
	t.once.Do(func() {
		if t.provider != nil {
			shutdownErr = t.provider.Shutdown(ctx)
		}
	})
	return shutdownErr
}

func (t *Telemetry) Metrics() *Metrics {
	if t == nil || t.metrics == nil {
		return NewMetrics(false)
	}
	return t.metrics
}

func DefaultMetrics() *Metrics {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultMetrics
}

func setDefaultMetrics(metrics *Metrics) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultMetrics = metrics
}

func StartHTTPSpan(ctx context.Context, method, route, requestID string) (context.Context, trace.Span) {
	return startSpan(ctx, "http.request", attribute.String("http.request.method", method), attribute.String("http.route", route), attribute.String("forgeflow.request_id", requestID))
}

func ExtractHTTPContext(ctx context.Context, header http.Header) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(header))
}

func SetHTTPRoute(span trace.Span, route string) {
	if span != nil {
		span.SetAttributes(attribute.String("http.route", boundedRoute(route)))
	}
}

func StartRunSpan(ctx context.Context, runID, traceID string) (context.Context, trace.Span) {
	return startSpan(ctx, "run.execute", attribute.String("forgeflow.run_id", runID), attribute.String("forgeflow.trace_id", traceID))
}

func StartNodeSpan(ctx context.Context, runID, traceID, nodeID string, attempt int) (context.Context, trace.Span) {
	return startSpan(ctx, "graph.node", attribute.String("forgeflow.run_id", runID), attribute.String("forgeflow.trace_id", traceID), attribute.String("forgeflow.node_id", nodeID), attribute.Int("forgeflow.attempt", attempt))
}

func StartModelSpan(ctx context.Context, provider, modelName, agent, nodeID string) (context.Context, trace.Span) {
	return startSpan(ctx, "model.generate", attribute.String("gen_ai.system", provider), attribute.String("gen_ai.request.model", modelName), attribute.String("forgeflow.agent", agent), attribute.String("forgeflow.node_id", nodeID))
}

func StartToolSpan(ctx context.Context, runID, traceID, toolName, toolVersion, agent, nodeID string) (context.Context, trace.Span) {
	return startSpan(ctx, "tool.execute", attribute.String("forgeflow.run_id", runID), attribute.String("forgeflow.trace_id", traceID), attribute.String("forgeflow.tool", toolName), attribute.String("forgeflow.tool_version", toolVersion), attribute.String("forgeflow.agent", agent), attribute.String("forgeflow.node_id", nodeID))
}

func EndSpan(span trace.Span, err error, status string) {
	if span == nil {
		return
	}
	if status != "" {
		span.SetAttributes(attribute.String("forgeflow.status", status))
	}
	if err != nil {
		span.RecordError(err)
	}
	span.End()
}

func startSpan(ctx context.Context, name string, attributes ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer(instrumentationName).Start(ctx, name, trace.WithAttributes(attributes...))
}

func emptyAs(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func JoinShutdownErrors(values ...error) error { return errors.Join(values...) }
