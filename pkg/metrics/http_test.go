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
		It("requires an API name", func() {
			client, err := NewInstrumentedHTTPClient("")
			Expect(err).To(MatchError("api name is required"))
			Expect(client).To(BeNil())
		})

		It("increments HTTPRequestCount for responses", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			labels := prometheus.Labels{
				apiLabel:       "test",
				operationLabel: "get_request-count-test",
			}
			before := testutil.ToFloat64(HTTPRequestCount.With(labels))

			client, err := NewInstrumentedHTTPClient("test")
			Expect(err).NotTo(HaveOccurred())

			response, err := client.Get(server.URL + "/request-count-test")
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			after := testutil.ToFloat64(HTTPRequestCount.With(labels))
			Expect(after - before).To(Equal(float64(1)))
		})

		It("records HTTPRequestDurationHistogram observations for responses", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			labels := prometheus.Labels{
				apiLabel:       "test",
				operationLabel: "get_request-duration-test",
			}
			before := histogramSampleCount(HTTPRequestDurationHistogram.With(labels))

			client, err := NewInstrumentedHTTPClient("test")
			Expect(err).NotTo(HaveOccurred())

			response, err := client.Get(server.URL + "/request-duration-test")
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			after := histogramSampleCount(HTTPRequestDurationHistogram.With(labels))
			Expect(after - before).To(Equal(uint64(1)))
		})

		It("increments HTTPErrorCount for 400 responses", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			}))
			defer server.Close()

			labels := prometheus.Labels{
				apiLabel: "test",
				"method": http.MethodGet,
				"code":   "400",
			}
			before := testutil.ToFloat64(HTTPErrorCount.With(labels))

			client, err := NewInstrumentedHTTPClient("test")
			Expect(err).NotTo(HaveOccurred())

			response, err := client.Get(server.URL)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			after := testutil.ToFloat64(HTTPErrorCount.With(labels))
			Expect(after - before).To(Equal(float64(1)))
		})

		It("increments HTTPErrorCount for 500 responses", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()

			labels := prometheus.Labels{
				apiLabel: "test",
				"method": http.MethodPost,
				"code":   "500",
			}
			before := testutil.ToFloat64(HTTPErrorCount.With(labels))

			client, err := NewInstrumentedHTTPClient("test")
			Expect(err).NotTo(HaveOccurred())

			response, err := client.Post(server.URL, "application/json", nil)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			after := testutil.ToFloat64(HTTPErrorCount.With(labels))
			Expect(after - before).To(Equal(float64(1)))
		})

		It("does not increment HTTPErrorCount for successful responses", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			labels := prometheus.Labels{
				apiLabel: "test",
				"method": http.MethodGet,
				"code":   "200",
			}
			before := testutil.ToFloat64(HTTPErrorCount.With(labels))

			client, err := NewInstrumentedHTTPClient("test")
			Expect(err).NotTo(HaveOccurred())

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
