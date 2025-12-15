package stackiterrors

import (
	"errors"
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oapiError "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas/wait"
)

var _ = Describe("Errors", func() {
	Describe("IsNotFound", func() {
		Context("when error is a NotFound error", func() {
			It("should return true", func() {
				err := &oapiError.GenericOpenAPIError{StatusCode: http.StatusNotFound}
				Expect(IsNotFound(err)).To(BeTrue())
			})
		})

		Context("when error is not a NotFound error", func() {
			It("should return false", func() {
				err := &oapiError.GenericOpenAPIError{StatusCode: http.StatusInternalServerError}
				Expect(IsNotFound(err)).To(BeFalse())
			})
		})

		Context("when error is not an OAPI error", func() {
			It("should return false", func() {
				err := errors.New("some error")
				Expect(IsNotFound(err)).To(BeFalse())
			})
		})

		Context("when error is nil", func() {
			It("should return false", func() {
				Expect(IsNotFound(nil)).To(BeFalse())
			})
		})
	})

	Describe("IgnoreNotFound", func() {
		Context("when error is a NotFound error", func() {
			It("should return nil", func() {
				err := &oapiError.GenericOpenAPIError{StatusCode: http.StatusNotFound}
				Expect(IgnoreNotFound(err)).To(Succeed())
			})
		})

		Context("when error is not a NotFound error", func() {
			It("should return the original error", func() {
				err := &oapiError.GenericOpenAPIError{StatusCode: http.StatusInternalServerError}
				Expect(IgnoreNotFound(err)).To(Equal(err))
			})
		})

		Context("when error is not an OAPI error", func() {
			It("should return the original error", func() {
				err := errors.New("some error")
				Expect(IgnoreNotFound(err)).To(Equal(err))
			})
		})

		Context("when error is nil", func() {
			It("should return nil", func() {
				Expect(IgnoreNotFound(nil)).To(Succeed())
			})
		})
	})

	Describe("WrapErrorWithResponseID", func() {
		Context("when request ID is provided", func() {
			It("should wrap the error with request ID", func() {
				err := errors.New("test error")
				reqID := "12345"
				expected := fmt.Errorf("[%s:%s]: %w", wait.XRequestIDHeader, reqID, err)
				Expect(WrapErrorWithResponseID(err, reqID)).To(Equal(expected))
			})
		})

		Context("when request ID is empty", func() {
			It("should return the original error", func() {
				err := errors.New("test error")
				Expect(WrapErrorWithResponseID(err, "")).To(Equal(err))
			})
		})

		Context("when error is nil", func() {
			It("should return nil", func() {
				Expect(WrapErrorWithResponseID(nil, "12345")).To(Succeed())
			})
		})
	})

	Describe("IsInvalidError", func() {
		Context("when error is a BadRequest error", func() {
			It("should return true", func() {
				err := &oapiError.GenericOpenAPIError{StatusCode: http.StatusBadRequest}
				Expect(IsInvalidError(err)).To(BeTrue())
			})
		})

		Context("when error is not a BadRequest error", func() {
			It("should return false", func() {
				err := &oapiError.GenericOpenAPIError{StatusCode: http.StatusInternalServerError}
				Expect(IsInvalidError(err)).To(BeFalse())
			})
		})

		Context("when error is not an OAPI error", func() {
			It("should return false", func() {
				err := errors.New("some error")
				Expect(IsInvalidError(err)).To(BeFalse())
			})
		})

		Context("when error is nil", func() {
			It("should return false", func() {
				Expect(IsInvalidError(nil)).To(BeFalse())
			})
		})
	})
})
