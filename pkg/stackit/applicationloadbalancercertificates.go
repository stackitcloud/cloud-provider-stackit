package stackit

import (
	"context"

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

func NewCertClient(cl *certsdk.APIClient) (CertificatesClient, error) {
	return &certClient{client: cl}, nil
}

func (cl certClient) GetCertificate(ctx context.Context, projectID, region, name string) (*certsdk.GetCertificateResponse, error) {
	cert, err := cl.client.DefaultAPI.GetCertificate(ctx, projectID, region, name).Execute()
	if isOpenAPINotFound(err) {
		return cert, ErrorNotFound
	}
	return cert, err
}

func (cl certClient) DeleteCertificate(ctx context.Context, projectID, region, name string) error {
	_, err := cl.client.DefaultAPI.DeleteCertificate(ctx, projectID, region, name).Execute()
	return err
}

func (cl certClient) CreateCertificate(ctx context.Context, projectID, region string, certificate *certsdk.CreateCertificatePayload) (*certsdk.GetCertificateResponse, error) {
	cert, err := cl.client.DefaultAPI.CreateCertificate(ctx, projectID, region).CreateCertificatePayload(*certificate).Execute()
	if isOpenAPINotFound(err) {
		return cert, ErrorNotFound
	}
	return cert, err
}

func (cl certClient) ListCertificate(ctx context.Context, projectID, region string) (*certsdk.ListCertificatesResponse, error) {
	certs, err := cl.client.DefaultAPI.ListCertificates(ctx, projectID, region).Execute()
	return certs, err
}
