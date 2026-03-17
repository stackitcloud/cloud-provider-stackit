package stackit

import (
	"context"
	"errors"
	"net/http"

	oapiError "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	certsdk "github.com/stackitcloud/stackit-sdk-go/services/certificates/v2api"
)

type CertificatesClient interface {
	GetCertificate(ctx context.Context, projectID, region, name string) (*certsdk.GetCertificateResponse, error)
	DeleteCertificate(ctx context.Context, projectID, region, name string) error
	CreateCertificate(ctx context.Context, projectID, region string, certificate *certsdk.CreateCertificatePayload) (*certsdk.GetCertificateResponse, error)
	ListCertificate(ctx context.Context, projectID, region string) (*certsdk.ListCertificatesResponse, error)
}

type certClient struct {
	client *certsdk.APIClient
}

var _ CertificatesClient = (*certClient)(nil)

func NewCertClient(cl *certsdk.APIClient) (Client, error) {
	return &certClient{client: cl}, nil
}

func (cl certClient) GetCertificate(ctx context.Context, projectID, region, name string) (*certsdk.GetCertificateResponse, error) {
	cert, err := cl.client.GetCertificateExecute(ctx, projectID, region, name)
	if isOpenAPINotFound(err) {
		return cert, ErrorNotFound
	}
	return cert, err
}

func (cl certClient) DeleteCertificate(ctx context.Context, projectID, region, name string) error {
	_, err := cl.client.DeleteCertificateExecute(ctx, projectID, region, name)
	return err
}

func (cl certClient) CreateCertificate(ctx context.Context, projectID, region string, certificate *certsdk.CreateCertificatePayload) (*certsdk.GetCertificateResponse, error) {
	cert, err := cl.client.CreateCertificate(ctx, projectID, region).CreateCertificatePayload(*certificate).Execute()
	if isOpenAPINotFound(err) {
		return cert, ErrorNotFound
	}
	return cert, err
}

func (cl certClient) ListCertificate(ctx context.Context, projectID, region string) (*certsdk.ListCertificatesResponse, error) {
	certs, err := cl.client.ListCertificates(ctx, projectID, region).Execute()
	return certs, err
}

func isOpenAPINotFound(err error) bool {
	apiErr := &oapiError.GenericOpenAPIError{}
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusNotFound
}

func IsNotFound(err error) bool {
	return errors.Is(err, ErrorNotFound)
}
