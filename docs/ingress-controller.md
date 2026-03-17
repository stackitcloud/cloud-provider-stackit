### Run the ALB Ingress controller locally
To run the controller on your local machine, ensure you have a valid kubeconfig pointing to the target Kubernetes cluster where the ALB resources should be managed.

##### Environment Variables
The controller requires specific configuration and credentials to interact with the STACKIT APIs and your network infrastructure. Set the following variables:
  - STACKIT_SERVICE_ACCOUNT_TOKEN: Your authentication token for performing CRUD operations via the ALB and Certificates SDK.
  - STACKIT_REGION: The STACKIT region where the infrastructure resides (e.g., eu01).
  - PROJECT_ID: The unique identifier of your STACKIT project where the ALB will be provisioned.
  - NETWORK_ID: The ID of the STACKIT network where the ALB will be provisioned.
```
export STACKIT_SERVICE_ACCOUNT_TOKEN=<your-token>
export STACKIT_REGION=<region>
export PROJECT_ID=<project-id>
export NETWORK_ID=<network-id>
```
Kubernetes Context
The controller uses the default Kubernetes client. Ensure your KUBECONFIG environment variable is set or your current context is correctly configured:
```
export KUBECONFIG=~/.kube/config
```
#### Run
Use the provided Makefile in the root of repository to start the controller:
```
make run
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
>NOTE: The service has to be of type NodePort to enable access to the nodes from the outside of the cluster.
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
