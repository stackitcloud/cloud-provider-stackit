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
