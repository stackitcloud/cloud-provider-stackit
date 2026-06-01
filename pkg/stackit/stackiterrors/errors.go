package stackiterrors

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	oapiError "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api/wait"
)

var ErrNotFound = errors.New("failed to find object")

func IsNotFound(err error) bool {
	var oAPIError *oapiError.GenericOpenAPIError
	if ok := errors.As(err, &oAPIError); !ok {
		return false
	}

	return oAPIError.StatusCode == http.StatusNotFound
}

func IsTooManyDevicesError(err error) bool {
	var oAPIError *oapiError.GenericOpenAPIError
	if ok := errors.As(err, &oAPIError); !ok {
		return false
	}

	// TODO: Improve this if possible
	return oAPIError.StatusCode == http.StatusForbidden &&
		strings.Contains(string(oAPIError.Body), "maximum allowed number of disk devices")
}

func IgnoreNotFound(err error) error {
	if IsNotFound(err) {
		return nil
	}
	return err
}

// WrapErrorWithResponseID wraps the error with the X-Request-Id but only if the error is not nil
func WrapErrorWithResponseID(err error, reqID string) error {
	if err == nil {
		return nil
	}
	// if the request id is empty we don't wrap the error
	if reqID == "" {
		return err
	}
	return fmt.Errorf("[%s:%s]: %w", wait.XRequestIDHeader, reqID, err)
}

func IsInvalidError(err error) bool {
	var oAPIError *oapiError.GenericOpenAPIError
	if ok := errors.As(err, &oAPIError); !ok {
		return false
	}

	return oAPIError.StatusCode == http.StatusBadRequest
}
