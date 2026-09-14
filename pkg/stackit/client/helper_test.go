package client

import (
	"context"
	"errors"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
)

var _ = Describe("withResponseID", func() {
	It("wraps API errors with trace and request IDs", func() {
		_, err := withResponseID(context.Background(), func(ctx context.Context) (int, error) {
			response, ok := ctx.Value(sdkconfig.ContextHTTPResponse).(**http.Response)
			Expect(ok).To(BeTrue())
			*response = &http.Response{Header: http.Header{
				"X-Trace-Id":   {"trace-123"},
				"X-Request-Id": {"request-456"},
			}}
			return 0, errors.New("api error")
		})

		Expect(err).To(MatchError("[X-Request-Id:request-456]: [X-Trace-Id:trace-123]: api error"))
	})
})
