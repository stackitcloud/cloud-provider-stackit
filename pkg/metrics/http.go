package metrics

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func NewInstrumentedHTTPClient(api string) *http.Client {
	return &http.Client{
		Transport: &InstrumentedRoundTripper{
			api:  api,
			base: http.DefaultTransport,
		},
	}
}

type InstrumentedRoundTripper struct {
	api  string
	base http.RoundTripper
}

func (rt *InstrumentedRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	operation := operationFromRequest(request)

	startTime := time.Now()
	response, err := rt.base.RoundTrip(request)
	duration := time.Since(startTime)

	statusCode := ""
	if response != nil {
		statusCode = strconv.Itoa(response.StatusCode)
	}

	HTTPRequestDurationHistogram.
		With(prometheus.Labels{
			apiLabel:       rt.api,
			methodLabel:    request.Method,
			operationLabel: operation,
			codeLabel:      statusCode,
		}).
		Observe(float64(duration.Seconds()))
	HTTPRequestCount.
		With(prometheus.Labels{
			apiLabel:       rt.api,
			methodLabel:    request.Method,
			operationLabel: operation,
			codeLabel:      statusCode,
		}).
		Inc()

	if response != nil && response.StatusCode >= 400 {
		HTTPErrorCount.With(prometheus.Labels{
			apiLabel:       rt.api,
			methodLabel:    request.Method,
			operationLabel: operation,
			codeLabel:      statusCode,
		}).Inc()
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
