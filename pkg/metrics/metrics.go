package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	cloudProviderMetricPrefix = "cloud_provider_stackit"
	loadBalancerSubSystem     = "lb"
	operationLabel            = "op"
)

var (
	LoadBalancerRequestCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace:   cloudProviderMetricPrefix,
		Subsystem:   loadBalancerSubSystem,
		Name:        "requests_total",
		Help:        "the number of requests to the load balancer API",
		ConstLabels: nil,
	}, []string{operationLabel})

	HTTPErrorCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace:   cloudProviderMetricPrefix,
		Name:        "http_errors_total",
		Help:        "Number of HTTP errors returned by external APIs",
		ConstLabels: nil,
	}, []string{"method", "code"})

	LoadBalancerResponseTimeHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   cloudProviderMetricPrefix,
		Subsystem:   loadBalancerSubSystem,
		Name:        "request_duration_seconds",
		Help:        "the response times of the load balancer API",
		ConstLabels: nil,
		Buckets:     nil,
	}, []string{operationLabel})
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
	LoadBalancerRequestCount.Describe(descs)
	HTTPErrorCount.Describe(descs)
	LoadBalancerResponseTimeHistogram.Describe(descs)
}

func (e *Exporter) collectCloudProvider(metrics chan<- prometheus.Metric) {
	LoadBalancerRequestCount.Collect(metrics)
	HTTPErrorCount.Collect(metrics)
	LoadBalancerResponseTimeHistogram.Collect(metrics)
}
