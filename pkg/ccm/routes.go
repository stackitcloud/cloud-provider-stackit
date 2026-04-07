package ccm

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/client"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	"golang.org/x/sync/errgroup"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	cloudprovider "k8s.io/cloud-provider"
)

// these are not included in the iaas sdk
const (
	routeDestinationTypeCIDRv4 = "cidrv4"
	routeDestinationTypeCIDRv6 = "cidrv6"
	routeNexthopTypeBlackhole  = "blackhole"
	routeNexthopTypeIPv4       = "ipv4"
	routeNexthopTypeIPv6       = "ipv6"
)

const (
	labelKeyRouteNameHint = "kubernetes.io_route_namehint"
	labelKeyRouteNodeName = "kubernetes.io_route_nodename"
	labelKeyClusterName   = "kubernetes.io_cluster"
)

type Routes struct {
	iaasClient     client.IaaSClient
	routingTableID string
}

// CreateRoute implements [cloudprovider.Routes].
func (r *Routes) CreateRoute(ctx context.Context, clusterName, nameHint string, route *cloudprovider.Route) error {
	rt, err := r.iaasClient.GetRoutingTable(ctx, r.routingTableID)
	if err != nil {
		return err
	}
	routes, err := r.routesFromCloudprovider(nameHint, clusterName, route)
	if err != nil {
		return fmt.Errorf("casting routes from cloudprovider.Route: %w", err)
	}

	existingRoutes, err := r.getExistingRoutes(ctx, clusterName, nameHint, string(route.TargetNode), rt.GetId())
	if err != nil {
		return fmt.Errorf("getting existing routes: %w", err)
	}

	newRoutes := sets.New(routes...).Difference(sets.New(existingRoutes...)).UnsortedList()
	newIaasRoutes := make([]iaas.Route, 0, len(newRoutes))
	for _, newRoute := range newRoutes {
		newIaasRoute, err := newRoute.ToIaasRoute(nameHint, clusterName)
		if err != nil {
			return err
		}
		newIaasRoutes = append(newIaasRoutes, newIaasRoute)
	}

	if err := r.iaasClient.AddRoutes(ctx, rt.GetId(), newIaasRoutes); err != nil {
		return fmt.Errorf("adding routes %s: %w", newRoutes, err)
	}
	return nil
}

// DeleteRoute implements [cloudprovider.Routes].
func (r *Routes) DeleteRoute(ctx context.Context, clusterName string, route *cloudprovider.Route) error {
	rt, err := r.iaasClient.GetRoutingTable(ctx, r.routingTableID)
	if err != nil {
		return err
	}
	routes, err := r.routesFromCloudprovider("", clusterName, route)
	if err != nil {
		return fmt.Errorf("casting routes from cloudprovider.Route: %w", err)
	}

	g, gctx := errgroup.WithContext(ctx)
	for _, route := range routes {
		g.Go(func() error {
			labels := routeLabels("", clusterName, route.NodeName)
			iaasRoutes, err := r.iaasClient.ListRoutes(ctx, rt.GetId(), labels)
			if err != nil {
				return fmt.Errorf("listing routes: %w", err)
			}

			for _, iaasRoute := range iaasRoutes {
				if err := r.iaasClient.DeleteRoute(gctx, rt.GetId(), iaasRoute.GetId()); err != nil {
					return fmt.Errorf("deleting route %s: %w", route, err)
				}
			}
			return nil
		})
	}

	return g.Wait()
}

// ListRoutes implements [cloudprovider.Routes].
func (r *Routes) ListRoutes(ctx context.Context, clusterName string) ([]*cloudprovider.Route, error) {
	rt, err := r.iaasClient.GetRoutingTable(ctx, r.routingTableID)
	if err != nil {
		return nil, err
	}

	routes, err := r.getExistingRoutes(ctx, clusterName, "", "", rt.GetId())
	if err != nil {
		return nil, fmt.Errorf("getting existing routes: %w", err)
	}

	return r.routesToCloudprovider(routes), nil
}

func (r *Routes) getExistingRoutes(ctx context.Context, clusterName, nameHint, targetNode, routingTableID string) ([]route, error) {
	labels := routeLabels(nameHint, clusterName, targetNode)
	iaasRoutes, err := r.iaasClient.ListRoutes(ctx, routingTableID, labels)
	if err != nil {
		return nil, err
	}
	routes := make([]route, 0, len(iaasRoutes))
	for _, iaasRoute := range iaasRoutes {
		route, err := r.routeFromIaas(iaasRoute)
		if err != nil {
			return nil, fmt.Errorf("casting route from iaas.Route: %w", err)
		}
		routes = append(routes, route)
	}
	return routes, nil
}

func (r *Routes) routesFromCloudprovider(nameHint, clusterName string, cloudroute *cloudprovider.Route) ([]route, error) {
	var routes []route
	for _, nodeAddr := range cloudroute.TargetNodeAddresses {
		if nodeAddr.Type != v1.NodeInternalIP {
			continue
		}

		nodeAddrIP, err := netip.ParseAddr(nodeAddr.Address)
		if err != nil {
			return nil, fmt.Errorf("parsing node address %s: %w", nodeAddr.Address, err)
		}

		destinationCIDR, err := netip.ParsePrefix(cloudroute.DestinationCIDR)
		if err != nil {
			return nil, fmt.Errorf("parsing route destinationCIDR %s: %w", cloudroute.DestinationCIDR, err)
		}
		routes = append(routes, route{
			Blackhole:       cloudroute.Blackhole,
			DestinationCIDR: destinationCIDR,
			NodeName:        string(cloudroute.TargetNode),
			NextHop:         nodeAddrIP,
		})
	}
	return routes, nil
}

func (r *Routes) routeFromIaas(iaasRoute iaas.Route) (route, error) {
	dest := iaasRoute.GetDestination()
	var destinationString string
	if dest.DestinationCIDRv4 != nil {
		destinationString = dest.DestinationCIDRv4.Value
	}
	if dest.DestinationCIDRv6 != nil {
		destinationString = dest.DestinationCIDRv6.Value
	}
	var destinationPrefix netip.Prefix
	if destinationString != "" {
		var err error
		destinationPrefix, err = netip.ParsePrefix(destinationString)
		if err != nil {
			return route{}, fmt.Errorf("parsing destination CIDR %s: %w ", destinationString, err)
		}
	}

	var nodeName string
	nodeNameInterface, ok := iaasRoute.GetLabels()[labelKeyRouteNodeName]
	if ok {
		nodeName = nodeNameInterface.(string)
	}

	nextHop := iaasRoute.GetNexthop()
	var nextHopString string
	if nextHop.NexthopIPv4 != nil {
		nextHopString = nextHop.NexthopIPv4.Value
	}
	if nextHop.NexthopIPv6 != nil {
		nextHopString = nextHop.NexthopIPv6.Value
	}
	var nextHopAddr netip.Addr
	if nextHopString != "" {
		var err error
		nextHopAddr, err = netip.ParseAddr(nextHopString)
		if err != nil {
			return route{}, fmt.Errorf("parsing nextHop %s: %w ", nextHopString, err)
		}
	}

	return route{
		Blackhole:       iaasRoute.GetNexthop().NexthopBlackhole != nil,
		DestinationCIDR: destinationPrefix,
		NodeName:        nodeName,
		NextHop:         nextHopAddr,
	}, nil
}

func (r *Routes) routesToCloudprovider(routes []route) []*cloudprovider.Route {
	nodeToAddr := map[string][]v1.NodeAddress{}
	nodeBlackhole := map[string]bool{}
	nodeToDestCIDR := map[string]string{}
	for _, route := range routes {
		nodeName := route.NodeName
		var nextHop string
		if !route.Blackhole {
			nextHop = route.NextHop.String()
		}

		if nextHop == "" {
			nodeBlackhole[nodeName] = true
			nodeToAddr[nodeName] = []v1.NodeAddress{}
		} else {
			addr := v1.NodeAddress{
				Type:    v1.NodeInternalIP,
				Address: nextHop,
			}
			nodeToAddr[nodeName] = append(nodeToAddr[nodeName], addr)
		}
		nodeToDestCIDR[nodeName] = route.DestinationCIDR.String()
	}

	cpRoutes := make([]*cloudprovider.Route, 0, len(nodeToAddr))
	for node, addrs := range nodeToAddr {
		cpRoutes = append(cpRoutes, &cloudprovider.Route{
			TargetNode:          types.NodeName(node),
			Blackhole:           nodeBlackhole[node],
			DestinationCIDR:     nodeToDestCIDR[node],
			TargetNodeAddresses: addrs,
		})
	}
	return cpRoutes
}

type route struct {
	NodeName        string
	NextHop         netip.Addr
	Blackhole       bool
	DestinationCIDR netip.Prefix
}

func (r route) String() string {
	sb := new(strings.Builder)
	fmt.Fprintf(sb, "node=%s, nextHop=%s ", r.NodeName, r.NextHop)
	if r.Blackhole {
		fmt.Fprint(sb, "blackhole")
	} else {
		fmt.Fprint(sb, "destinationCIDR=%s", r.DestinationCIDR)
	}
	return sb.String()
}

func (r route) ToIaasRoute(nameHint, clusterName string) (iaas.Route, error) {
	nextHop, err := r.iaasNextHop()
	if err != nil {
		return iaas.Route{}, err
	}

	dest, err := r.iaasRouteDestination()
	if err != nil {
		return iaas.Route{}, err
	}

	return iaas.Route{
		Destination: dest,
		Nexthop:     nextHop,
		Labels:      routeLabels(nameHint, clusterName, r.NodeName).ToSDK(),
	}, nil
}

func (r route) iaasRouteDestination() (iaas.RouteDestination, error) {
	var dest iaas.RouteDestination

	switch len(r.DestinationCIDR.Addr().AsSlice()) {
	case 4:
		dest.DestinationCIDRv4 = &iaas.DestinationCIDRv4{
			Type:  routeDestinationTypeCIDRv4,
			Value: r.DestinationCIDR.String(),
		}
	case 16:
		dest.DestinationCIDRv6 = &iaas.DestinationCIDRv6{
			Type:  routeDestinationTypeCIDRv6,
			Value: r.DestinationCIDR.String(),
		}
	default:
		return dest, fmt.Errorf("unknown ip type %s", r.DestinationCIDR.Addr())
	}
	return dest, nil
}

func (r route) iaasNextHop() (iaas.RouteNexthop, error) {
	nextHop := iaas.RouteNexthop{}
	if r.Blackhole {
		nextHop.NexthopBlackhole = &iaas.NexthopBlackhole{
			Type: routeNexthopTypeBlackhole,
		}
		return nextHop, nil
	}
	switch len(r.NextHop.AsSlice()) {
	case 4:
		nextHop.NexthopIPv4 = &iaas.NexthopIPv4{
			Type:  routeNexthopTypeIPv4,
			Value: r.NextHop.String(),
		}
	case 16:
		nextHop.NexthopIPv6 = &iaas.NexthopIPv6{
			Type:  routeNexthopTypeIPv6,
			Value: r.NextHop.String(),
		}
	default:
		return nextHop, fmt.Errorf("unknown ip type %s", r.NextHop)
	}

	return nextHop, nil
}

type iaaslabels map[string]string

func routeLabels(nameHint, clusterName, targetNode string) iaaslabels {
	l := iaaslabels{
		labelKeyClusterName: clusterName,
	}
	if targetNode != "" {
		l[labelKeyRouteNodeName] = targetNode
	}
	// nameHint is only available during create
	if nameHint != "" {
		l[labelKeyRouteNameHint] = nameHint
	}
	return l
}

func (l iaaslabels) ToSDK() map[string]any {
	sdkLabels := make(map[string]any, len(l))
	for k, v := range l {
		sdkLabels[k] = v
	}
	return sdkLabels
}
