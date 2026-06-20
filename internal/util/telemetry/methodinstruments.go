// Package telemetry provides shared OpenTelemetry helpers for adapter packages.
// It is infrastructure-only and must not import any module (domain) packages.
//
// Every adapter that wraps a repository or service with metrics should use
// NewMethodInstruments to register the standard four-instrument set
// (calls, successes, failures, duration) and MsElapsed to record timing.
package telemetry

import (
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// MethodInstruments holds the four OTel instruments used to observe a single
// adapter method.
//
//   - Calls     — incremented before the call; total invocation count.
//   - Successes — incremented after a nil-error return.
//   - Failures  — incremented after a non-nil-error return.
//   - Duration  — histogram of wall-clock time in milliseconds.
//
// Note: Calls == Successes + Failures at all times, making it easy to
// cross-check counters and compute an error rate.
type MethodInstruments struct {
	Calls     metric.Int64Counter
	Successes metric.Int64Counter
	Failures  metric.Int64Counter
	Duration  metric.Float64Histogram // unit: ms
}

// NewMethodInstruments registers the four standard OTel instruments for a
// named method.
//
// name is the metric name prefix, e.g.
// "baasparse.transactionaudit.create_transaction". The suffixes .calls,
// .successes, .failures, and .duration are appended automatically.
//
// Returns an error if any instrument fails to register with the provided Meter,
// which typically indicates a misconfigured or no-op meter provider.
func NewMethodInstruments(m metric.Meter, name string) (MethodInstruments, error) {
	calls, err := m.Int64Counter(name+".calls",
		metric.WithDescription("Total invocations of "+name+"."))
	if err != nil {
		return MethodInstruments{}, fmt.Errorf("telemetry: register %s.calls: %w", name, err)
	}

	successes, err := m.Int64Counter(name+".successes",
		metric.WithDescription("Invocations of "+name+" that completed without error."))
	if err != nil {
		return MethodInstruments{}, fmt.Errorf("telemetry: register %s.successes: %w", name, err)
	}

	failures, err := m.Int64Counter(name+".failures",
		metric.WithDescription("Invocations of "+name+" that returned an error."))
	if err != nil {
		return MethodInstruments{}, fmt.Errorf("telemetry: register %s.failures: %w", name, err)
	}

	duration, err := m.Float64Histogram(name+".duration",
		metric.WithDescription("Execution time of "+name+" in milliseconds."),
		metric.WithUnit("ms"))
	if err != nil {
		return MethodInstruments{}, fmt.Errorf("telemetry: register %s.duration: %w", name, err)
	}

	return MethodInstruments{
		Calls:     calls,
		Successes: successes,
		Failures:  failures,
		Duration:  duration,
	}, nil
}

// MsElapsed returns the wall-clock time elapsed since start as a float64
// in milliseconds. Use this as the value passed to MethodInstruments.Duration.Record.
func MsElapsed(start time.Time) float64 {
	return float64(time.Since(start)) / float64(time.Millisecond)
}
