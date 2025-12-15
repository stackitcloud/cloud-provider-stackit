package stackiterrors

import (
	"errors"
	"fmt"
	"net/http"

	oapiError "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas/wait"
)

var ErrNotFound = errors.New("failed to find object")

func IsNotFound(err error) bool {
	var oAPIError *oapiError.GenericOpenAPIError
	if ok := errors.As(err, &oAPIError); !ok {
		return false
	}

	return oAPIError.StatusCode == http.StatusNotFound
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
