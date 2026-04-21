package stackit

import (
	"context"
	"net/http"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/stackiterrors"
	"github.com/stackitcloud/stackit-sdk-go/core/runtime"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	wait "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api/wait"
)

func (os *iaasClient) GetInstanceByID(ctx context.Context, instanceID string) (*iaas.Server, error) {
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	server, err := os.iaas.GetServer(ctxWithHTTPResp, os.projectID, os.region, instanceID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(wait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}
		return nil, err
	}
	return server, nil
}
