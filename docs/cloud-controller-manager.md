# Cloud controller manager

## Overview

The cloud controller manager implements the [Kubernetes cloud-controller-manager contract](https://kubernetes.io/docs/concepts/architecture/cloud-controller/#functions-of-the-ccm).

### Route controller

Route controller is used to 
> The route controller is responsible for configuring routes in the cloud appropriately so that containers on different nodes in your Kubernetes cluster can communicate with each other.

For more information check the [Kubernetes documentation](https://kubernetes.io/docs/concepts/architecture/cloud-controller/#route-controller).

In order to use it, make sure to specify a routing table in your config.

```yaml
route:
  routingTableId: "my-rt"
```

The route controller can be used in SNA and VPC based clusters. The routing table specified in the config must be present in the respective SNA/VPC.
Whether VPC or SNA is used is determined based on the config:

Example SNA config:
```yaml
global:
  areaId: foo
  orgId: xyz
```


Example VPC config:
```yaml
global:
  vpcId: my-vpc
```

#### Multiple clusters in the same routing table

To be able to make multiple clusters support native routing of Pod IPs regard the following limitations:
- Pod CIDRs of all clusters (`--cluster-cidr` flag in cloud-controller-manager) must be dissect, overlapping ranges may result misbehavior. The route-controller may add a route with the same pod CIDR using a different nexthop.
- Unique cluster name (`--cluster-name`). Each cloud-controller-manager must use a unique cluster name as the routes are managed based on cluster name.

### Node controller

The node controller is responsible for updating Node objects when new servers are created in STACKIT infrastructure by obtaining information about the servers.

For more information check the [Kubernetes documentation](https://kubernetes.io/docs/concepts/architecture/cloud-controller/#node-controller).

#### Multi Network

If a server has NICs connected to multiple networks, you can designate the primary network for [Node Addresses](https://kubernetes.io/docs/reference/node/node-status/#addresses) by setting the default network in the config:

```yaml
instance:
  # either network name or id
  defaultNetwork: "foo"
```

This ensures the IP address for that network's NIC is listed first in the [Node status](https://kubernetes.io/docs/reference/node/node-status/#addresses).
