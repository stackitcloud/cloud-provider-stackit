package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	cloudProviderMetricPrefix = "cloud_provider_stackit"
	apiLabel                  = "api"
	methodLabel               = "method"
	codeLabel                 = "status_code"
	operationLabel            = "op"

	APINameLoadBalancer = "loadbalancer"
	APINameIaaS         = "iaas"
)

var (
	HTTPRequestCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace:   cloudProviderMetricPrefix,
		Name:        "http_requests_total",
		Help:        "The number of requests to external APIs",
		ConstLabels: nil,
	}, []string{apiLabel, operationLabel})

	HTTPErrorCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace:   cloudProviderMetricPrefix,
		Name:        "http_errors_total",
		Help:        "Number of HTTP errors returned by external APIs",
		ConstLabels: nil,
	}, []string{apiLabel, methodLabel, codeLabel})

	HTTPRequestDurationHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   cloudProviderMetricPrefix,
		Name:        "http_request_duration_seconds",
		Help:        "The response times of external API requests",
		ConstLabels: nil,
		Buckets:     nil,
	}, []string{apiLabel, operationLabel})
)

type Exporter struct {
}

func NewExporter() *Exporter {
	e := &Exporter{}

	return e
}

func (e *Exporter) Describe(descs chan<- *prometheus.Desc) {
	e.describeCloudProvider(descs)
}

func (e *Exporter) Collect(metrics chan<- prometheus.Metric) {
	e.collectCloudProvider(metrics)
}

func (e *Exporter) describeCloudProvider(descs chan<- *prometheus.Desc) {
	HTTPRequestCount.Describe(descs)
	HTTPErrorCount.Describe(descs)
	HTTPRequestDurationHistogram.Describe(descs)
}

func (e *Exporter) collectCloudProvider(metrics chan<- prometheus.Metric) {
	HTTPRequestCount.Collect(metrics)
	HTTPErrorCount.Collect(metrics)
	HTTPRequestDurationHistogram.Collect(metrics)
}
