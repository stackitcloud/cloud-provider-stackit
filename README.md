# cloud-provider-stackit

This repository includes:

- Cloud Controller Manager
- Kubernetes Resources for the Manager
- Ginko bootstrapped Test Suite
- Prow Job in ske-ci-infra for build

For further information: take the [hcloud-example](https://github.com/hetznercloud/hcloud-cloud-controller-manager/tree/main) as reference.

Does not include:

- readyz and healthz
- Kubernetes Client with self authorization by `inClusterConfig`

TODOs:

- remove all `nolint:golint,all` from Code
- switch to Prow-Cluster `ske-prow-trusted` in the `ske-ci-infra` repository
- Rollout the Kubernetes Resources (Automatically)
- This implementation allows untagged clouds, verify if `hasClusterID()` is needed and then remove this flag in `deployment.yaml`
