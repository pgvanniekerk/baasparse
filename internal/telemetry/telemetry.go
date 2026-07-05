// Package telemetry wires OpenTelemetry for baasparse (TS 16 §16.8): metrics via
// a Prometheus exporter (served at /metrics) plus optional OTLP push, and traces
// via OTLP when an endpoint is configured. Logs stay on slog→stdout (collected
// by the platform). When no OTLP endpoint is set, traces are cheap no-ops and
// metrics are still available at /metrics, so the engine runs fine without a
// collector present.
package telemetry

import (
	"context"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	promexp "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Config configures telemetry.
type Config struct {
	ServiceName    string
	ServiceVersion string
	InstanceID     string
	OTLPEndpoint   string // OTLP/HTTP endpoint (empty disables trace export)
	Insecure       bool
}

// Telemetry holds the providers and exposes the metrics handler + a tracer.
type Telemetry struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *metric.MeterProvider
	metricsHandler http.Handler
	shutdowns      []func(context.Context) error
}

// Init sets up the global OTel providers.
func Init(ctx context.Context, cfg Config) (*Telemetry, error) {
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
		semconv.ServiceInstanceID(cfg.InstanceID),
	))
	if err != nil {
		res = resource.Default()
	}

	t := &Telemetry{}

	// --- Metrics: Prometheus exporter (always) + optional OTLP push ---
	promReader, err := promexp.New()
	if err != nil {
		return nil, err
	}
	mpOpts := []metric.Option{metric.WithResource(res), metric.WithReader(promReader)}
	t.meterProvider = metric.NewMeterProvider(mpOpts...)
	otel.SetMeterProvider(t.meterProvider)
	t.metricsHandler = promhttp.Handler()
	t.shutdowns = append(t.shutdowns, t.meterProvider.Shutdown)

	// --- Traces: OTLP when an endpoint is configured, else global no-op ---
	if cfg.OTLPEndpoint != "" {
		exp, err := otlptracehttp.New(ctx, endpointOptions(cfg.OTLPEndpoint, cfg.Insecure)...)
		if err != nil {
			return nil, err
		}
		t.tracerProvider = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exp),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(t.tracerProvider)
		t.shutdowns = append(t.shutdowns, t.tracerProvider.Shutdown)
	}
	return t, nil
}

// MetricsHandler returns the Prometheus /metrics handler.
func (t *Telemetry) MetricsHandler() http.Handler { return t.metricsHandler }

// Tracer returns a named tracer (a no-op tracer when tracing is disabled).
func (t *Telemetry) Tracer(name string) trace.Tracer { return otel.Tracer(name) }

// Shutdown flushes and stops the providers.
func (t *Telemetry) Shutdown(ctx context.Context) {
	for i := len(t.shutdowns) - 1; i >= 0; i-- {
		_ = t.shutdowns[i](ctx)
	}
}

func endpointOptions(endpoint string, insecure bool) []otlptracehttp.Option {
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(endpoint)}
		if strings.HasPrefix(endpoint, "http://") {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		return opts
	}
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint)}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	return opts
}
