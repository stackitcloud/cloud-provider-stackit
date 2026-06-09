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

### Expose your deployment via Ingress
Check out our sample manifests to quickly deploy and expose your applications:
1. **[Deployment](../samples/ingress/deployment.yaml)**: A sample web server.
2. **[Service](../samples/ingress/service.yaml)**: Exposes the pods (must be of type `NodePort`).
3. **[IngressClass](../samples/ingress/ingress-class.yaml)**: Specifies the `stackit.cloud/alb-ingress` controller.
4. **[Ingress](../samples/ingress/ingress.yaml)**: Routes external traffic to your service.

### Ingress to ALB Mapping
All Ingress resources that reference the same IngressClass are grouped together and provisioned on a single, shared Application Load Balancer (ALB). If you need separate ALBs (for instance, if you want to assign different static IP addresses or need one public and one internal ALB) then you must create a distinct IngressClass for each one.

### Ingress Rule Evaluation Order
When multiple Ingress resources share the same ALB, the controller must sort them to determine the order in which routing rules are evaluated. By default, resources are sorted by their CreationTimestamp, meaning older Ingresses are evaluated first.

If you need to explicitly prioritize certain rules over others, you can override this default behavior using the `alb.stackit.cloud/priority` annotation on your Ingress resource. Ingresses with a higher priority value are evaluated first. If multiple Ingresses share the same priority score, the controller falls back to sorting them by their creation timestamp.

Note that this sorting only applies across different Ingress resources. The top-to-bottom sequence of rules and paths defined within a single Ingress YAML is not preserved and is processed non-deterministically. If you need to preserve the exact top-to-bottom order specified in your YAML, you must separate them into distinct Ingress resources and use the priority annotation.

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

| Annotation | Allowed On | Requirement | Description |
| :--- | :--- | :--- | :--- |
| `alb.stackit.cloud/external-address` | IngressClass | Optional | Uses a specific STACKIT public IP instead of an ephemeral one. |
| `alb.stackit.cloud/internal` | IngressClass | Optional | If `true`, the ALB is not exposed via a public IP. |
| `alb.stackit.cloud/plan-id` | IngressClass | Optional | Sets the service plan for the ALB. |
| `alb.stackit.cloud/http-port` | IngressClass, Ingress | Optional | Specifies the custom HTTP port. |
| `alb.stackit.cloud/https-port` | IngressClass, Ingress | Optional | Specifies the custom HTTPS port. |
| `alb.stackit.cloud/https-only` | IngressClass, Ingress | Optional | If `true`, the Ingress will not be reachable via HTTP. |
| `alb.stackit.cloud/websocket` | IngressClass, Ingress | Optional | Enables global WebSocket support. |
| `alb.stackit.cloud/web-application-firewall-name`| IngressClass, Ingress | Optional | Attaches a specific WAF configuration. |
| `alb.stackit.cloud/cookie-persistence-name` | IngressClass, Ingress | Optional | Sets the name for session cookie persistence. |
| `alb.stackit.cloud/cookie-persistence-ttl-seconds`| IngressClass, Ingress | Optional | Sets the TTL (in seconds) for cookie persistence. |
| `alb.stackit.cloud/priority` | Ingress | Optional | Defines the evaluation priority of the Ingress. |
| `alb.stackit.cloud/traget-pool-tls-enabled` | IngressClass, Ingress, Service | Optional | Enables TLS bridging using OS trusted CAs. |
| `alb.stackit.cloud/traget-pool-tls-custom-ca` | IngressClass, Ingress, Service | Optional | Enables TLS bridging with a custom CA. |
| `alb.stackit.cloud/traget-pool-tls-skip-certificate-validation`| IngressClass, Ingress, Service | Optional | Enables TLS bridging but skips certificate validation. |