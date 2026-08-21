package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

var _ = Describe("Metrics", func() {
	Describe("getSDKOperationName", func() {
		var (
			server     *httptest.Server
			host       string
			component  = "test"
			iaasClient *iaas.APIClient
		)

		BeforeEach(func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			url, err := url.Parse(server.URL)
			Expect(err).NotTo(HaveOccurred())
			host = url.Host

			HTTPRequestCount.Reset()
			HTTPErrorCount.Reset()
			HTTPRequestDurationHistogram.Reset()

			iaasClient, err = iaas.NewAPIClient(
				sdkconfig.WithHTTPClient(NewHTTPClient(component)),
				sdkconfig.WithEndpoint(server.URL),
				sdkconfig.WithoutAuthentication(),
			)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			server.Close()
		})

		It("should return DeleteVolume as operation", func() {
			err := iaasClient.DefaultAPI.DeleteVolume(context.TODO(), uuid.New().String(), "", uuid.New().String()).Execute()
			Expect(err).NotTo(HaveOccurred())

			labels := prometheus.Labels{
				hostLabel:      host,
				componentLabel: component,
				methodLabel:    "DELETE",
				operationLabel: "DeleteVolume",
				codeLabel:      "200",
			}

			Expect(testutil.ToFloat64(HTTPRequestCount.With(labels))).To(Equal(float64(1)))
		})

		It("should return DeleteServer as operation", func() {
			err := iaasClient.DefaultAPI.DeleteServer(context.TODO(), uuid.New().String(), "", uuid.New().String()).Execute()
			Expect(err).NotTo(HaveOccurred())

			labels := prometheus.Labels{
				hostLabel:      host,
				componentLabel: component,
				methodLabel:    "DELETE",
				operationLabel: "DeleteServer",
				codeLabel:      "200",
			}

			Expect(testutil.ToFloat64(HTTPRequestCount.With(labels))).To(Equal(float64(1)))
		})
	})

	Describe("InstrumentedRoundTripper", func() {
		var (
			server     *httptest.Server
			httpClient *http.Client
			host       string
			component  = "test"
		)

		BeforeEach(func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/404":
					w.WriteHeader(http.StatusNotFound)
				case "/500":
					w.WriteHeader(http.StatusInternalServerError)
				case "/400":
					w.WriteHeader(http.StatusBadRequest)
				default:
					w.WriteHeader(http.StatusOK)
				}
			}))
			url, err := url.Parse(server.URL)
			Expect(err).NotTo(HaveOccurred())
			host = url.Host
			httpClient = NewHTTPClient(component)

			HTTPRequestCount.Reset()
			HTTPErrorCount.Reset()
			HTTPRequestDurationHistogram.Reset()
		})

		AfterEach(func() {
			server.Close()
		})

		It("increments HTTPRequestCount for responses", func() {
			labels := prometheus.Labels{
				hostLabel:      host,
				componentLabel: component,
				methodLabel:    "GET",
				operationLabel: UnknownOperation,
				codeLabel:      "200",
			}

			response, err := httpClient.Get(server.URL + "/request-count-test")
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			Expect(testutil.ToFloat64(HTTPRequestCount.With(labels))).To(Equal(float64(1)))
		})

		It("records HTTPRequestDurationHistogram observations for responses", func() {
			labels := prometheus.Labels{
				hostLabel:      host,
				componentLabel: component,
				methodLabel:    "GET",
				operationLabel: UnknownOperation,
				codeLabel:      "200",
			}

			response, err := httpClient.Get(server.URL + "/request-duration-test")
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			Expect(histogramSampleCount(HTTPRequestDurationHistogram.With(labels))).To(Equal(uint64(1)))
		})

		It("increments HTTPErrorCount for error responses (400, 404, 500)", func() {
			labels400 := prometheus.Labels{
				hostLabel:      host,
				componentLabel: component,
				methodLabel:    http.MethodGet,
				operationLabel: UnknownOperation,
				codeLabel:      "400",
			}
			labels404 := prometheus.Labels{
				hostLabel:      host,
				componentLabel: component,
				methodLabel:    http.MethodGet,
				operationLabel: UnknownOperation,
				codeLabel:      "404",
			}
			labels500 := prometheus.Labels{
				hostLabel:      host,
				componentLabel: component,
				methodLabel:    http.MethodPost,
				operationLabel: UnknownOperation,
				codeLabel:      "500",
			}

			response1, err := httpClient.Get(server.URL + "/400")
			Expect(err).NotTo(HaveOccurred())
			defer response1.Body.Close()

			response2, err := httpClient.Get(server.URL + "/404")
			Expect(err).NotTo(HaveOccurred())
			defer response2.Body.Close()

			response3, err := httpClient.Post(server.URL+"/500", "application/json", nil)
			Expect(err).NotTo(HaveOccurred())
			defer response3.Body.Close()

			Expect(testutil.ToFloat64(HTTPErrorCount.With(labels400))).To(Equal(float64(1)))
			Expect(testutil.ToFloat64(HTTPErrorCount.With(labels404))).To(Equal(float64(1)))
			Expect(testutil.ToFloat64(HTTPErrorCount.With(labels500))).To(Equal(float64(1)))
		})

		It("does not increment HTTPErrorCount for successful responses", func() {
			labels := prometheus.Labels{
				hostLabel:      host,
				componentLabel: component,
				methodLabel:    http.MethodGet,
				operationLabel: UnknownOperation,
				codeLabel:      "200",
			}

			response, err := httpClient.Get(server.URL)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			Expect(testutil.ToFloat64(HTTPErrorCount.With(labels))).To(Equal(float64(0)))
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
