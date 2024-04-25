# market

## Description
**market** is a component of **Terminus OS** based on **app-service** and **market-server**, principally tasked with managing the installation, upgrade, and uninstallation of Application, Model, and Recommend among other related operations.

The **Application** is a Custom Resource (CR) defined by k8s Custom Resource Definition (CRD). When a user initiates an installation, it is automatically created by the Application Controller.

## Getting Started
You’ll need a Kubernetes cluster to run against. You’ll need market running on the Kubernetes cluster. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster. 

### Build backend
1. Build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/market-backend:tag
```
### Build frontend
[frontend](./frontend/README.md)