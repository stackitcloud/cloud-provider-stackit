package metrics

import (
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus"
)

const UnknownOperation = "UnknownOperation"

func NewHTTPClient(componentName string) *http.Client {
	return WrapHTTPClient(http.DefaultClient, componentName)
}

func WrapHTTPClient(client *http.Client, componentName string) *http.Client {
	if client == nil {
		return nil
	}
	wrappedClient := *client

	baseTransport := client.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}

	// Chain your instrumented round tripper
	wrappedClient.Transport = &InstrumentedRoundTripper{
		base:          baseTransport,
		componentName: componentName,
	}

	return &wrappedClient
}

type InstrumentedRoundTripper struct {
	base          http.RoundTripper
	componentName string
}

func (rt *InstrumentedRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	startTime := time.Now()
	response, err := rt.base.RoundTrip(request)
	duration := time.Since(startTime)

	statusCode := "network_error"
	if response != nil {
		statusCode = strconv.Itoa(response.StatusCode)
	}

	// request.Host is optional so we can fallback to request.URL.Host (if available)
	host := request.Host
	if host == "" && request.URL != nil {
		host = request.URL.Host
	}

	labels := prometheus.Labels{
		componentLabel: rt.componentName,
		hostLabel:      host,
		methodLabel:    request.Method,
		operationLabel: getSDKOperationName(),
		codeLabel:      statusCode,
	}

	HTTPRequestDurationHistogram.With(labels).Observe(duration.Seconds())
	HTTPRequestCount.With(labels).Inc()

	isHTTPError := response != nil && response.StatusCode >= 400
	isNetworkError := err != nil

	if isHTTPError || isNetworkError {
		HTTPErrorCount.With(labels).Inc()
	}

	return response, err
}

// getSDKOperationName returns the name of the STACKIT SDK function. To do this the function gets the last 10 callers and checks
// for functions from the stackitcloud/stackit-sdk-go. It fall back to UnknownOperation if no function was found.
func getSDKOperationName() string {
	pc := make([]uintptr, 10)

	// Skip 3 because the first 3 are always Callers, getSDKOperationName, RoundTrip.
	n := runtime.Callers(3, pc)
	if n == 0 {
		return UnknownOperation
	}

	frames := runtime.CallersFrames(pc[:n])
	moreFrames := true
	for moreFrames {
		var frame runtime.Frame
		frame, moreFrames = frames.Next()

		if !strings.Contains(frame.Function, "stackitcloud/stackit-sdk-go") {
			continue
		}

		parts := strings.Split(frame.Function, ".")
		if len(parts) > 0 {
			funcName := parts[len(parts)-1]

			// Skip function names with 0 len
			// Skip Execute, because there is a function with more detailed name
			// Skip RoundTrip, because this only the RoundTrip for the AuthFlow
			if funcName == "" ||
				funcName == "Execute" ||
				funcName == "RoundTrip" {
				continue
			}

			// Skip Private functions
			r, _ := utf8.DecodeRuneInString(funcName)
			if !unicode.IsUpper(r) {
				continue
			}

			return strings.TrimSuffix(funcName, "Execute")
		}
	}

	return UnknownOperation
}
