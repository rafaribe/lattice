// Package metrics provides an OpenTelemetry metrics adapter with Prometheus export.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Metrics holds OTEL meter instruments for lattice.
type Metrics struct {
	Meter            metric.Meter
	RequestsTotal    metric.Int64Counter
	RequestDuration  metric.Float64Histogram
	ActiveEngines    metric.Int64UpDownCounter
	ProxyRequests    metric.Int64Counter
	ProxyErrors      metric.Int64Counter
	HeartbeatsTotal  metric.Int64Counter
	ModelsAvailable  metric.Int64Gauge
}

// New initializes the OTEL meter provider with Prometheus exporter and creates instruments.
func New() (*Metrics, http.Handler, error) {
	exporter, err := prometheus.New()
	if err != nil {
		return nil, nil, err
	}

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)

	meter := provider.Meter("lattice")

	requestsTotal, _ := meter.Int64Counter("lattice_http_requests_total",
		metric.WithDescription("Total HTTP requests handled"),
	)
	requestDuration, _ := meter.Float64Histogram("lattice_http_request_duration_seconds",
		metric.WithDescription("HTTP request duration in seconds"),
	)
	activeEngines, _ := meter.Int64UpDownCounter("lattice_engines_active",
		metric.WithDescription("Number of active engines in the grid"),
	)
	proxyRequests, _ := meter.Int64Counter("lattice_proxy_requests_total",
		metric.WithDescription("Total inference requests proxied to engines"),
	)
	proxyErrors, _ := meter.Int64Counter("lattice_proxy_errors_total",
		metric.WithDescription("Total proxy errors (engine unreachable, timeout)"),
	)
	heartbeatsTotal, _ := meter.Int64Counter("lattice_heartbeats_total",
		metric.WithDescription("Total heartbeat requests received"),
	)
	modelsAvailable, _ := meter.Int64Gauge("lattice_models_available",
		metric.WithDescription("Number of unique models available in the grid"),
	)

	m := &Metrics{
		Meter:           meter,
		RequestsTotal:   requestsTotal,
		RequestDuration: requestDuration,
		ActiveEngines:   activeEngines,
		ProxyRequests:   proxyRequests,
		ProxyErrors:     proxyErrors,
		HeartbeatsTotal: heartbeatsTotal,
		ModelsAvailable: modelsAvailable,
	}

	return m, promhttp.Handler(), nil
}
