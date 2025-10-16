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

	LoadBalancerErrorCount = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace:   cloudProviderMetricPrefix,
		Subsystem:   loadBalancerSubSystem,
		Name:        "errors_total",
		Help:        "the number of server errors reported when calling the load balancer API",
		ConstLabels: nil,
	})

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
	LoadBalancerErrorCount.Describe(descs)
	LoadBalancerResponseTimeHistogram.Describe(descs)
}

func (e *Exporter) collectCloudProvider(metrics chan<- prometheus.Metric) {
	LoadBalancerRequestCount.Collect(metrics)
	LoadBalancerErrorCount.Collect(metrics)
	LoadBalancerResponseTimeHistogram.Collect(metrics)
}
