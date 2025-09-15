package metrics

import (
	"net/http"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
})
