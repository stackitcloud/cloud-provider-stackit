package metrics

import (
	"net/http"
	"net/http/httptest"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

var _ = Describe("Metrics", func() {
	DescribeTable("operationFromRequest", func(method, path, expected string) {
		requestURL, _ := url.Parse("https://host" + path)
		request := &http.Request{
			Method: method,
			URL:    requestURL,
		}
		op := operationFromRequest(request)
		Expect(op).To(Equal(expected))
	},
		Entry("post token", "POST", "/token", "post_token"),
		Entry("get load-balancers", "GET", "/v2/projects/6-a-4-8-c/regions/eu01/load-balancers", "get_load-balancers"),
		Entry("get load-balancers instance", "GET", "/v2/projects/6-a-4-8-c/regions/eu01/load-balancers/id", "get_load-balancers_instance"),
	)

	Describe("InstrumentedRoundTripper", func() {
		It("increments HTTPRequestCount for responses", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			labels := prometheus.Labels{
				apiLabel:       "test",
				operationLabel: "get_request-count-test",
			}
			before := testutil.ToFloat64(HTTPRequestCount.With(labels))

			client := NewInstrumentedHTTPClient("test")

			response, err := client.Get(server.URL + "/request-count-test")
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			after := testutil.ToFloat64(HTTPRequestCount.With(labels))
			Expect(after - before).To(Equal(float64(1)))
		})

		It("records HTTPRequestDurationHistogram observations for responses", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			labels := prometheus.Labels{
				apiLabel:       "test",
				operationLabel: "get_request-duration-test",
			}
			before := histogramSampleCount(HTTPRequestDurationHistogram.With(labels))

			client := NewInstrumentedHTTPClient("test")

			response, err := client.Get(server.URL + "/request-duration-test")
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			after := histogramSampleCount(HTTPRequestDurationHistogram.With(labels))
			Expect(after - before).To(Equal(uint64(1)))
		})

		It("increments HTTPErrorCount for error responses (400, 404, 500)", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				if r.URL.Path == "/404" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.WriteHeader(http.StatusBadRequest)
			}))
			defer server.Close()

			labels400 := prometheus.Labels{
				apiLabel:    "test",
				methodLabel: http.MethodGet,
				codeLabel:   "400",
			}
			labels404 := prometheus.Labels{
				apiLabel:    "test",
				methodLabel: http.MethodGet,
				codeLabel:   "404",
			}
			labels500 := prometheus.Labels{
				apiLabel:    "test",
				methodLabel: http.MethodPost,
				codeLabel:   "500",
			}
			before400 := testutil.ToFloat64(HTTPErrorCount.With(labels400))
			before404 := testutil.ToFloat64(HTTPErrorCount.With(labels404))
			before500 := testutil.ToFloat64(HTTPErrorCount.With(labels500))

			client := NewInstrumentedHTTPClient("test")

			response1, err := client.Get(server.URL)
			Expect(err).NotTo(HaveOccurred())
			defer response1.Body.Close()

			response2, err := client.Get(server.URL + "/404")
			Expect(err).NotTo(HaveOccurred())
			defer response2.Body.Close()

			response3, err := client.Post(server.URL, "application/json", nil)
			Expect(err).NotTo(HaveOccurred())
			defer response3.Body.Close()

			after400 := testutil.ToFloat64(HTTPErrorCount.With(labels400))
			after404 := testutil.ToFloat64(HTTPErrorCount.With(labels404))
			after500 := testutil.ToFloat64(HTTPErrorCount.With(labels500))

			Expect(after400 - before400).To(Equal(float64(1)))
			Expect(after404 - before404).To(Equal(float64(1)))
			Expect(after500 - before500).To(Equal(float64(1)))
			Expect((after400 - before400) + (after404 - before404) + (after500 - before500)).To(Equal(float64(3)))
		})

		It("does not increment HTTPErrorCount for successful responses", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			labels := prometheus.Labels{
				apiLabel:    "test",
				methodLabel: http.MethodGet,
				codeLabel:   "200",
			}
			before := testutil.ToFloat64(HTTPErrorCount.With(labels))

			client := NewInstrumentedHTTPClient("test")

			response, err := client.Get(server.URL)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			after := testutil.ToFloat64(HTTPErrorCount.With(labels))
			Expect(after - before).To(Equal(float64(0)))
		})
	})
})

func histogramSampleCount(observer prometheus.Observer) uint64 {
	metric, ok := observer.(prometheus.Metric)
	Expect(ok).To(BeTrue())

	dtoMetric := &dto.Metric{}
	Expect(metric.Write(dtoMetric)).To(Succeed())

	return dtoMetric.GetHistogram().GetSampleCount()
}
