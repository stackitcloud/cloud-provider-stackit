package stackit

import (
	"context"
	"net/http"

	stackiterrors "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/errors"
	"github.com/stackitcloud/stackit-sdk-go/core/runtime"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas/wait"
)

func (os *iaasClient) GetInstanceByID(ctx context.Context, instanceID string) (*iaas.Server, error) {
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	server, err := os.iaas.GetServer(ctxWithHTTPResp, os.projectID, instanceID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(wait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}
		return nil, err
	}
	return server, nil
}
