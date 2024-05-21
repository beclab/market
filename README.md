# Market
![Market](https://docs.jointerminus.com/images/how-to/terminus/market/discover.jpg)

The **Market** is a built-in application of **Terminus OS**. It's an implementation of the decentralized and permissionless [Open Application Distribution Protocol](https://docs.jointerminus.com/overview/protocol/market.html).

With Market, you can one-click install various apps, recommendation algorithms, and large language models from Terminus and third-party developers.

## Overview
**Market** is built on **app-service** and **market-server**. Its primary role is to manage the installation, upgrade, and uninstallation of Apps, Models, and Recommendations, among other related operations.

The **Application** in Terminus is a Custom Resource (CR) defined by the k8s Custom Resource Definition (CRD). When a user initiates an installation, it's automatically created by the Application Controller.

## Getting Started
If you want to use Terminus, there's no need to run this service separately. Just install Terminus, and it will be included in the system.

Alternatively, if you want to run a standalone Market instance, please refer to the installation guide below.

## How to install
To run Market, you'll need a Kubernetes cluster. You can use [KIND](https://sigs.k8s.io/kind) for a local testing cluster, or use a remote cluster.

### Build backend
1. Build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/market-backend:tag
```
### Build frontend
Please refer to [frontend](./frontend/README.md)