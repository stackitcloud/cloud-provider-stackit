package stackit

import (
	"context"
	"net/http"

	csiError "github.com/stackitcloud/cloud-provider-stackit/pkg/util/errors"
	"github.com/stackitcloud/stackit-sdk-go/core/runtime"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas/wait"
)

func (os *iaasClient) GetInstanceByID(ctx context.Context, instanceID string) (*iaas.Server, error) {
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	server, err := os.iaas.GetServer(ctxWithHTTPResp, os.projectID, instanceID).Execute()
	if err != nil {
		reqID := httpResp.Header.Get(wait.XRequestIDHeader)
		return nil, csiError.WrapErrorWithResponseID(err, reqID)
	}
	return server, nil
}
