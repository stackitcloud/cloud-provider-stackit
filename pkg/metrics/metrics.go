package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricPrefix   = "stackit_api"
	componentLabel = "component"
	hostLabel      = "host"
	methodLabel    = "method"
	operationLabel = "operation"
	codeLabel      = "status_code"
)

var (
	HTTPRequestCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricPrefix,
		Name:      "http_requests_total",
		Help:      "The number of requests to external APIs",
	}, []string{componentLabel, hostLabel, methodLabel, operationLabel, codeLabel})

	HTTPErrorCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricPrefix,
		Name:      "http_errors_total",
		Help:      "Number of HTTP errors returned by external APIs",
	}, []string{componentLabel, hostLabel, methodLabel, operationLabel, codeLabel})

	HTTPRequestDurationHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricPrefix,
		Name:      "http_request_duration_seconds",
		Help:      "The response times of external API requests",
	}, []string{componentLabel, hostLabel, methodLabel, operationLabel, codeLabel})
)

type Exporter struct {
}

func NewExporter() *Exporter {
	return &Exporter{}
}

func (e *Exporter) Describe(descs chan<- *prometheus.Desc) {
	HTTPRequestCount.Describe(descs)
	HTTPErrorCount.Describe(descs)
	HTTPRequestDurationHistogram.Describe(descs)
}

func (e *Exporter) Collect(metrics chan<- prometheus.Metric) {
	HTTPRequestCount.Collect(metrics)
	HTTPErrorCount.Collect(metrics)
	HTTPRequestDurationHistogram.Collect(metrics)
}
