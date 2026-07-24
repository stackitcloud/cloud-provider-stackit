package sdk

import (
	authorization "github.com/stackitcloud/stackit-sdk-go/services/authorization/v2api"
	"k8s.io/utils/set"
)

var ServiceAccountRoles = []string{
	"iaas.isolated-network.admin", // required by the infra controller
	"iaas.network.admin",          // required by the infra controller
	"nlb.admin",                   // required by the SelfHostedShootExposure controller
}

func GetMembersForRoles(subject string, roles set.Set[string]) []authorization.Member {
	members := make([]authorization.Member, 0, roles.Len())
	for role := range roles {
		members = append(members, authorization.Member{
			Role:    role,
			Subject: subject,
		})
	}
	return members
}
