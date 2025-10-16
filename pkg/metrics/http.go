package metrics

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func NewInstrumentedHTTPClient() *http.Client {
	return &http.Client{
		Transport: &InstrumentedRoundTripper{http.DefaultTransport},
	}
}

type InstrumentedRoundTripper struct {
	base http.RoundTripper
}

func (rt *InstrumentedRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	operation := operationFromRequest(request)

	startTime := time.Now()
	response, err := rt.base.RoundTrip(request)
	duration := time.Since(startTime)

	LoadBalancerResponseTimeHistogram.
		With(prometheus.Labels{operationLabel: operation}).
		Observe(float64(duration.Seconds()))
	LoadBalancerRequestCount.
		With(prometheus.Labels{operationLabel: operation}).
		Inc()

	if response != nil && response.StatusCode >= http.StatusInternalServerError {
		LoadBalancerErrorCount.Inc()
	}

	return response, err
}

func operationFromRequest(request *http.Request) string {
	verb := strings.ToLower(request.Method)

	pathElements := strings.Split(request.URL.Path, "/")
	if len(pathElements) <= 1 {
		return fmt.Sprintf("%s_%s", verb, request.URL.Path)
	}
	// since the path starts with a '/', the first path element is the empty string
	pathElements = pathElements[1:]

	// the subject is always the last or the second to last element:
	// .../subject -> even number of path elements
	// .../subject/<some-id> -> odd number of path elements
	var subject string
	if len(pathElements) == 1 {
		// edge case
		subject = pathElements[0]
	} else if len(pathElements)%2 == 0 {
		// even
		subject = pathElements[len(pathElements)-1]
	} else {
		// odd
		subject = pathElements[len(pathElements)-2] + "_instance"
	}

	return fmt.Sprintf("%s_%s", verb, subject)
}
