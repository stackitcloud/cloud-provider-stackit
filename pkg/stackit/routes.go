package stackit

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
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
)

type RouteClient struct {
	iaasClient iaas.DefaultApi
	areaID     string
	orgID      string
	region     string
}

// CreateRoute implements [cloudprovider.Routes].
func (r *RouteClient) CreateRoute(ctx context.Context, clusterName, nameHint string, route *cloudprovider.Route) error {
	rt, err := r.getRoutingTable(ctx, clusterName)
	if err != nil {
		return err
	}
	routes, err := r.routesFromCloudprovider(nameHint, clusterName, route)
	if err != nil {
		return err
	}

	existingRoutes, err := r.getExistingRoutes(ctx, clusterName, nameHint, rt.GetId(), route)
	if err != nil {
		return err
	}

	payload := iaas.NewAddRoutesToRoutingTablePayloadWithDefaults()
	payload.SetItems(r.newRoutes(existingRoutes, routes))

	_, err = r.iaasClient.AddRoutesToRoutingTable(ctx, r.orgID, r.areaID, r.region, rt.GetId()).
		AddRoutesToRoutingTablePayload(*payload).
		Execute()

	return err
}

// DeleteRoute implements [cloudprovider.Routes].
func (r *RouteClient) DeleteRoute(ctx context.Context, clusterName string, route *cloudprovider.Route) error {
	rt, err := r.getRoutingTable(ctx, clusterName)
	if err != nil {
		return err
	}
	routes, err := r.routesFromCloudprovider("", clusterName, route)
	if err != nil {
		return err
	}
	g, gctx := errgroup.WithContext(ctx)
	for _, iaasRoute := range routes {
		g.Go(r.iaasClient.DeleteRouteFromRoutingTable(gctx, r.orgID, r.areaID, r.region, rt.GetId(), iaasRoute.GetId()).Execute)
	}

	return g.Wait()
}

// ListRoutes implements [cloudprovider.Routes].
func (r *RouteClient) ListRoutes(ctx context.Context, clusterName string) ([]*cloudprovider.Route, error) {
	rt, err := r.getRoutingTable(ctx, clusterName)
	if err != nil {
		return nil, err
	}
	resp, err := r.iaasClient.ListRoutesOfRoutingTable(ctx, r.orgID, r.areaID, r.region, rt.GetId()).
		Execute()
	if err != nil {
		return nil, err
	}
	return r.iaasRoutesToCloudprovider(resp.GetItems()), nil
}

func (r *RouteClient) getExistingRoutes(ctx context.Context, clusterName, nameHint, routingTableID string, route *cloudprovider.Route) ([]iaas.Route, error) {
	resp, err := r.iaasClient.ListRoutesOfRoutingTable(ctx, r.orgID, r.areaID, r.region, routingTableID).
		LabelSelector(r.routeLabels(nameHint, clusterName, string(route.TargetNode)).Selector()).
		Execute()
	if err != nil {
		return nil, err
	}
	return resp.GetItems(), nil
}

// newRoutes returns only routes that are not yet present
func (r *RouteClient) newRoutes(existingRoutes, newRoutes []iaas.Route) []iaas.Route {
	// remove server side fields so that set can match routes
	for i := range existingRoutes {
		existingRoutes[i].Id = nil
		existingRoutes[i].CreatedAt = nil
		existingRoutes[i].UpdatedAt = nil
	}
	return sets.New(existingRoutes...).Difference(sets.New(newRoutes...)).UnsortedList()
}

func (r *RouteClient) getRoutingTable(ctx context.Context, clusterName string) (*iaas.RoutingTable, error) {
	resp, err := r.iaasClient.ListRoutingTablesOfArea(ctx, r.orgID, r.areaID, r.region).
		LabelSelector(r.routingTableLabels(clusterName).Selector()).
		Execute()
	if err != nil {
		return nil, err
	}
	tables := resp.GetItems()
	if len(tables) == 0 {
		return nil, ErrorNotFound
	}
	return &tables[0], err
}

func (r *RouteClient) routesFromCloudprovider(nameHint, clusterName string, route *cloudprovider.Route) ([]iaas.Route, error) {
	var routes []iaas.Route
	for _, nodeAddr := range route.TargetNodeAddresses {
		if nodeAddr.Type != v1.NodeInternalIP {
			continue
		}

		nodeAddrIP, err := netip.ParseAddr(nodeAddr.Address)
		if err != nil {
			return nil, err
		}
		nextHop, err := r.routeNextHop(nodeAddrIP, route.Blackhole)
		if err != nil {
			return nil, err
		}

		dest, err := r.routeDestination(route.DestinationCIDR)
		if err != nil {
			return nil, err
		}

		route := iaas.Route{
			Destination: dest,
			Nexthop:     nextHop,
			Labels:      new(r.routeLabels(nameHint, clusterName, string(route.TargetNode)).ToSDK()),
		}
		routes = append(routes, route)
	}
	return routes, nil
}

func (r *RouteClient) routingTableLabels(clusterName string) labels {
	return labels{
		labelKeyClusterName: clusterName,
	}
}

func (r *RouteClient) routeLabels(nameHint, clusterName, targetNode string) labels {
	l := labels{
		labelKeyClusterName:   clusterName,
		labelKeyRouteNodeName: targetNode,
	}
	// nameHint is only available during create
	if nameHint != "" {
		l[labelKeyRouteNameHint] = nameHint
	}
	return l
}

func (r *RouteClient) routeDestination(destinationCIDR string) (*iaas.RouteDestination, error) {
	routeprefix, err := netip.ParsePrefix(destinationCIDR)
	if err != nil {
		return nil, err
	}

	switch len(routeprefix.Addr().AsSlice()) {
	case 4:
		return &iaas.RouteDestination{
			DestinationCIDRv4: &iaas.DestinationCIDRv4{
				Type:  new(routeDestinationTypeCIDRv4),
				Value: new(routeprefix.String()),
			},
		}, nil
	case 16:
		return &iaas.RouteDestination{
			DestinationCIDRv6: &iaas.DestinationCIDRv6{
				Type:  new(routeDestinationTypeCIDRv6),
				Value: new(routeprefix.String()),
			},
		}, nil
	default:
		return nil, fmt.Errorf("unknown ip type %s", routeprefix.Addr())
	}
}

func (r *RouteClient) routeNextHop(nodeIP netip.Addr, blackhole bool) (*iaas.RouteNexthop, error) {
	nextHop := &iaas.RouteNexthop{}
	if blackhole {
		nextHop.NexthopBlackhole = &iaas.NexthopBlackhole{
			Type: new(routeNexthopTypeBlackhole),
		}
		return nextHop, nil
	}
	switch len(nodeIP.AsSlice()) {
	case 4:
		nextHop.NexthopIPv4 = &iaas.NexthopIPv4{
			Type:  new(routeNexthopTypeIPv4),
			Value: new(nodeIP.String()),
		}
	case 16:
		nextHop.NexthopIPv6 = &iaas.NexthopIPv6{
			Type:  new(routeNexthopTypeIPv6),
			Value: new(nodeIP.String()),
		}
	default:
		return nil, fmt.Errorf("unknown ip type %s", nodeIP)
	}

	return nextHop, nil
}

func (r *RouteClient) iaasRoutesToCloudprovider(routes []iaas.Route) []*cloudprovider.Route {
	nodeToAddr := map[string][]v1.NodeAddress{}
	nodeBlackhole := map[string]bool{}
	nodeToDestCIDR := map[string]string{}
	for _, route := range routes {
		nodeName := route.GetLabels()[labelKeyRouteNodeName].(string)
		nextHop := r.nextHop(&route)

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
		nodeToDestCIDR[nodeName] = r.destinationCIDR(&route)
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

func (r *RouteClient) nextHop(route *iaas.Route) string {
	if route.Nexthop.NexthopBlackhole != nil {
		return ""
	}
	if nextHopV4 := route.Nexthop.NexthopIPv4; nextHopV4 != nil {
		return nextHopV4.GetValue()
	}
	if nextHopV6 := route.Nexthop.NexthopIPv6; nextHopV6 != nil {
		return nextHopV6.GetValue()
	}
	return ""
}

func (r *RouteClient) destinationCIDR(route *iaas.Route) string {
	if route.Destination.DestinationCIDRv4 != nil {
		return route.Destination.DestinationCIDRv4.GetValue()
	}
	if route.Destination.DestinationCIDRv6 != nil {
		return route.Destination.DestinationCIDRv6.GetValue()
	}
	return ""
}
