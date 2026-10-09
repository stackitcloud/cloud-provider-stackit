package ccm

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	stackitclient "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/client"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	corev1 "k8s.io/api/core/v1"
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
	iaasClient     stackitclient.IaaSClient
	routingTableID string
}

// CreateRoute implements [cloudprovider.Routes].
func (r *Routes) CreateRoute(ctx context.Context, clusterName, nameHint string, route *cloudprovider.Route) error {
	routes, err := r.routesFromCloudprovider(route)
	if err != nil {
		return fmt.Errorf("casting routes from cloudprovider.Route: %w", err)
	}

	existingRoutes, err := r.getExistingRoutes(ctx, clusterName, nameHint, string(route.TargetNode), r.routingTableID)
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

	if len(newRoutes) == 0 {
		return nil
	}

	if err := r.iaasClient.AddRoutes(ctx, r.routingTableID, newIaasRoutes); err != nil {
		return fmt.Errorf("adding routes %s: %w", newRoutes, err)
	}
	return nil
}

// DeleteRoute implements [cloudprovider.Routes].
func (r *Routes) DeleteRoute(ctx context.Context, clusterName string, cloudproviderRoute *cloudprovider.Route) error {
	labels := routeLabels("", clusterName, string(cloudproviderRoute.TargetNode))
	iaasRoutes, err := r.iaasClient.ListRoutes(ctx, r.routingTableID, labels)
	if err != nil {
		return err
	}

	existingRoutes := map[route]string{}
	for _, iaasRoute := range iaasRoutes {
		route, err := routeFromIaas(&iaasRoute)
		if err != nil {
			return err
		}
		existingRoutes[*route] = iaasRoute.GetId()
	}

	routes, err := r.routesFromCloudprovider(cloudproviderRoute)
	if err != nil {
		return fmt.Errorf("casting routes from cloudprovider.Route: %w", err)
	}
	var deleteErr error
	for _, route := range routes {
		id, ok := existingRoutes[route]
		if ok {
			deleteErr = errors.Join(err, r.iaasClient.DeleteRoute(ctx, r.routingTableID, id))
		}
	}

	return deleteErr
}

// ListRoutes implements [cloudprovider.Routes].
func (r *Routes) ListRoutes(ctx context.Context, clusterName string) ([]*cloudprovider.Route, error) {
	routes, err := r.getExistingRoutes(ctx, clusterName, "", "", r.routingTableID)
	if err != nil {
		return nil, fmt.Errorf("getting existing routes: %w", err)
	}

	return routes.ToCloudProvider(), nil
}

func (r *Routes) getExistingRoutes(ctx context.Context, clusterName, nameHint, targetNode, routingTableID string) (routes, error) {
	labels := routeLabels(nameHint, clusterName, targetNode)
	iaasRoutes, err := r.iaasClient.ListRoutes(ctx, routingTableID, labels)
	if err != nil {
		return nil, err
	}
	routes := make(routes, 0, len(iaasRoutes))
	for _, iaasRoute := range iaasRoutes {
		route, err := routeFromIaas(&iaasRoute)
		if err != nil {
			return nil, fmt.Errorf("casting route from iaas.Route: %w", err)
		}
		routes = append(routes, *route)
	}
	return routes, nil
}

// routesFromCloudprovider parses [cloudprovider.Route] into the in-memory route representation.
// A [cloudprovider.Route] can results in multiple in-memory routes since we need 1 route per node IP
func (r *Routes) routesFromCloudprovider(cloudroute *cloudprovider.Route) (routes, error) {
	var routes routes
	destinationCIDR, err := netip.ParsePrefix(cloudroute.DestinationCIDR)
	if err != nil {
		return nil, fmt.Errorf("parsing route destinationCIDR %s: %w", cloudroute.DestinationCIDR, err)
	}
	for _, nodeAddr := range cloudroute.TargetNodeAddresses {
		if nodeAddr.Type != corev1.NodeInternalIP {
			continue
		}

		nodeAddrIP, err := netip.ParseAddr(nodeAddr.Address)
		if err != nil {
			return nil, fmt.Errorf("parsing node address %s: %w", nodeAddr.Address, err)
		}

		routes = append(routes, route{
			DestinationCIDR: destinationCIDR,
			NodeName:        string(cloudroute.TargetNode),
			NextHop:         nodeAddrIP,
		})
	}
	if cloudroute.Blackhole {
		routes = append(routes, route{
			Blackhole:       true,
			DestinationCIDR: destinationCIDR,
			NodeName:        string(cloudroute.TargetNode),
		})
	}
	return routes, nil
}

// route represents the internal data representation of routes.
// It can be used to convert to [iaas.Route] as well as to [cloudprovider.Route]
type route struct {
	NodeName        string
	NextHop         netip.Addr
	Blackhole       bool
	DestinationCIDR netip.Prefix
}

// routes is a slice of route to allow methods
type routes []route

func (r routes) ToCloudProvider() []*cloudprovider.Route {
	nodeInfoMap := r.aggregateNodeRoutes()
	cpRoutes := make([]*cloudprovider.Route, 0, len(nodeInfoMap))
	for node, info := range nodeInfoMap {
		for _, destCIDR := range info.DestCIDRs {
			cpRoutes = append(cpRoutes, &cloudprovider.Route{
				TargetNode:          types.NodeName(node),
				TargetNodeAddresses: info.Addresses.UnsortedList(),
				DestinationCIDR:     destCIDR,
				Blackhole:           info.Blackhole,
				// EnableNodeAddresses = true will make the route controller reconcile routes if node.Status.Address changes.
				// Since this will trigger create - delete calls if we return TargetNodeAddresses that miss certain Addresses (like Hostname),
				// we will not leverage this feature as we cannot get all Addresses from the routes only.
				EnableNodeAddresses: false,
			})
		}
	}
	return cpRoutes
}

type nodeRouteInfo struct {
	Blackhole bool
	Addresses sets.Set[corev1.NodeAddress]
	DestCIDRs []string
}

func (r routes) aggregateNodeRoutes() map[string]*nodeRouteInfo {
	nodeMap := make(map[string]*nodeRouteInfo)

	for _, route := range r {
		node, exists := nodeMap[route.NodeName]
		if !exists {
			node = &nodeRouteInfo{
				Addresses: sets.New[corev1.NodeAddress](),
			}
			nodeMap[route.NodeName] = node
		}

		node.Blackhole = route.Blackhole

		if !route.NextHop.IsUnspecified() {
			node.Addresses = sets.Insert(node.Addresses, corev1.NodeAddress{
				Type:    corev1.NodeInternalIP,
				Address: route.NextHop.String(),
			})
		}

		node.DestCIDRs = append(node.DestCIDRs, route.DestinationCIDR.String())
	}

	return nodeMap
}

func (r route) String() string {
	sb := new(strings.Builder)
	fmt.Fprintf(sb, "node=%s, nextHop=%s ", r.NodeName, r.NextHop)
	if r.Blackhole {
		fmt.Fprint(sb, "blackhole")
	} else {
		fmt.Fprintf(sb, "destinationCIDR=%s", r.DestinationCIDR)
	}
	return sb.String()
}

func (r *route) ToIaasRoute(nameHint, clusterName string) (iaas.Route, error) {
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

func (r *route) iaasRouteDestination() (iaas.RouteDestination, error) {
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

func (r *route) iaasNextHop() (iaas.RouteNexthop, error) {
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

func routeFromIaas(iaasRoute *iaas.Route) (*route, error) {
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
			return nil, fmt.Errorf("parsing destination CIDR %s: %w ", destinationString, err)
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
			return nil, fmt.Errorf("parsing nextHop %s: %w ", nextHopString, err)
		}
	}

	return &route{
		Blackhole:       iaasRoute.GetNexthop().NexthopBlackhole != nil,
		DestinationCIDR: destinationPrefix,
		NodeName:        nodeName,
		NextHop:         nextHopAddr,
	}, nil
}

func routeLabels(nameHint, clusterName, targetNode string) stackitclient.LabelMap {
	l := stackitclient.LabelMap{
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
