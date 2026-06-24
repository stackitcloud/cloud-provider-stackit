# Cloud controller manager

## Overview

The cloud controller manager implements the [Kubernetes cloud-controller-manager contract](https://kubernetes.io/docs/concepts/architecture/cloud-controller/#functions-of-the-ccm).

### Node controller

#### Multi Network

If a server has NICs connected to multiple networks, you can designate the primary network for Node Addresses by setting the default network in the config:

```yaml
instance:
  # either network name or id
  defaultNetwork: "foo"
```

This ensures the IP address for that network's NIC is listed first in the node status.
