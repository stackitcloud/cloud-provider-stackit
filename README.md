# Cloud Provider STACKIT

[![GitHub License](https://img.shields.io/github/license/stackitcloud/stackit-sdk-go)](https://www.apache.org/licenses/LICENSE-2.0)

This repository contains varous components for running Kubernetes on STACKIT.
This provider allows your Kubernetes cluster to integrate directly with STACKIT APIs.

## Features

This repository hosts the following components:

- Cloud Controller Manager (CCM)
- STACKIT CSI driver
- Kubernetes Resources for the Manager
- Ginko bootstrapped Test Suite

Does not include:

- `readyz` and `healthz`
- Kubernetes Client with self authorization by `inClusterConfig`

## User Documentation

- Usage
  - [Load Balancer](docs/load-balancer.md)
  - [CSI Driver](docs/csi-driver.md)
- Administration
  - [Deployment](docs/deployment.md)
- Development
  - [Testing](docs/testing.md)
  - [Release Procedure](docs/release-procedure.md)
