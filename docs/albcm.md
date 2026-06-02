# Application Load Balancer Controller Manager

The Application Load Balancer Controller Manager (ALBCM) manages ALBs from within a Kubernetes cluster.
Currently, the Ingress API is supported.
Support for Gateway API is planned.

##### Environment Variables

The controller requires specific configuration and credentials to interact with the STACKIT APIs and your network infrastructure. Set the following variables:

- STACKIT_REGION: The STACKIT region where the infrastructure resides (e.g., eu01).
- PROJECT_ID: The unique identifier of your STACKIT project where the ALB will be provisioned.
- NETWORK_ID: The ID of the STACKIT network where the ALB will be provisioned.
- In addition, the ALBCM supports all environment variable support by the STACKIT SDK. This includes authentication.

The controller uses the default Kubernetes client. Ensure your KUBECONFIG environment variable is set or your current context is correctly configured:
```
export KUBECONFIG=~/.kube/config
```

### Create your deployment and expose it via Ingress

1. Create your k8s deployment, here’s an example of a simple http web server:

```
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: httpbin-deployment
  name: httpbin-deployment
  namespace: default
spec:
  replicas: 2
  selector:
    matchLabels:
      app: httpbin-deployment
  template:
    metadata:
      labels:
        app: httpbin-deployment
    spec:
      containers:
      - image: kennethreitz/httpbin
        name: httpbin
        ports:
        - containerPort: 80
```

2. Now, create a k8s service so that the traffic can be routed to the pods:

```
apiVersion: v1
kind: Service
metadata:
  labels:
    app: httpbin-deployment
  name: httpbin
  namespace: default
spec:
  ports:
  - port: 80
    protocol: TCP
    targetPort: 80
    nodePort: 30000
  selector:
    app: httpbin-deployment
  type: NodePort
```

> NOTE: The service has to be of type NodePort to enable access to the nodes from the outside of the cluster.

3. Create an IngressClass that specifies the ALB Ingress controller:

```
apiVersion: networking.k8s.io/v1
kind: IngressClass
metadata:
  namespace: default
  name: alb-01
spec:
  controller: stackit.cloud/alb-ingress
```

4. Lastly, create an ingress resource that references the previously created IngressClass:

```
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: alb-ingress
  namespace: default
spec:
  ingressClassName: alb-01
  rules:
  - host: example.gg
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: httpbin
            port:
              number: 80
```

### WebSockets Support
You can enable WebSocket support for your applications by adding a specific annotation to your Ingress resource. Note that in this initial release, enabling this annotation applies globally to all routing rules defined within that specific Ingress.

To enable it, add the alb.stackit.cloud/websocket annotation to your Ingress metadata:
```
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: alb-ingress
  namespace: default
  annotations:
    alb.stackit.cloud/websocket: "true"
spec:
  # ... rest of your ingress spec
```

### Web Application Firewall (WAF)
You can secure your applications by attaching a WAF configuration using the `alb.stackit.cloud/web-application-firewall-name` annotation:

IngressClass Level (Global): Applies the WAF configuration to all listeners created by any Ingress using this class. Note: This takes precedence and overwrites any WAF configuration specified on individual Ingress resources.

Ingress Level (Specific): Applies the WAF configuration only to the listeners created by that individual Ingress, provided the IngressClass does not enforce a global WAF.

Example:
```
metadata:
  annotations:
    alb.stackit.cloud/web-application-firewall-name: "my-waf-config"
```

### Supported Annotations

| Annotation | Allowed On | Description |
| :--- | :--- | :--- |
| `alb.stackit.cloud/external-address` | IngressClass | Uses a specific STACKIT floating IP instead of an ephemeral one. |
| `alb.stackit.cloud/internal` | IngressClass | If `true`, the ALB is not exposed via a public IP. |
| `alb.stackit.cloud/plan-id` | IngressClass | Sets the service plan for the ALB. |
| `alb.stackit.cloud/http-port` | IngressClass, Ingress | Specifies the custom HTTP port. |
| `alb.stackit.cloud/https-port` | IngressClass, Ingress | Specifies the custom HTTPS port. |
| `alb.stackit.cloud/https-only` | IngressClass, Ingress | If `true`, the Ingress will not be reachable via HTTP. |
| `alb.stackit.cloud/websocket` | IngressClass, Ingress | Enables global WebSocket support. |
| `alb.stackit.cloud/web-application-firewall-name`| IngressClass, Ingress | Attaches a specific WAF configuration. |
| `alb.stackit.cloud/cookie-persistence-name` | IngressClass, Ingress | Sets the name for session cookie persistence. |
| `alb.stackit.cloud/cookie-persistence-ttl-seconds`| IngressClass, Ingress | Sets the TTL (in seconds) for cookie persistence. |
| `alb.stackit.cloud/priority` | IngressClass, Ingress | Defines the evaluation priority of the Ingress. |
| `alb.stackit.cloud/traget-pool-tls-enabled` | IngressClass, Ingress, Service | Enables TLS bridging using OS trusted CAs. |
| `alb.stackit.cloud/traget-pool-tls-custom-ca` | IngressClass, Ingress, Service | Enables TLS bridging with a custom CA. |
| `alb.stackit.cloud/traget-pool-tls-skip-certificate-validation`| IngressClass, Ingress, Service | Enables TLS bridging but skips certificate validation. |