package ccm

import (
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
)

// TODO: move most of the logic from stackit routeclient into here and remove references to cloudprovider package in stackit package
type routes struct {
	*stackit.RouteClient
}
