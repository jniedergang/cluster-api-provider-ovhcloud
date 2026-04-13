# cluster-api-provider-ovhcloud

[![lint](https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/actions/workflows/lint.yml/badge.svg)](https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/actions/workflows/lint.yml)
[![test](https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/actions/workflows/test.yml/badge.svg)](https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/actions/workflows/test.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

A [Cluster API](https://cluster-api.sigs.k8s.io/) infrastructure provider
for [OVH Public Cloud](https://www.ovhcloud.com/en/public-cloud/).

It lets you declare a Kubernetes cluster on OVH as a Kubernetes resource:
provision the network, an Octavia load balancer, control plane and worker
instances, and clean everything up on deletion.

## Features

- **Public Cloud lifecycle**: instances, vRack private networks, subnets,
  Octavia load balancers (small/medium/large/xl), SSH keys, block storage
- **Floating IP**: optional public endpoint for the API server
- **BYOI**: use any image from the OVH catalog (`Ubuntu 22.04`,
  `Debian 12`, ...) or upload your own (e.g. openSUSE, SLES) via Glance
- **RKE2 + kubeadm**: ready-to-use templates for both bootstrap providers
- **ClusterClass**: topology-based clusters in ~30 lines of YAML
- **Webhook validation**: optional admission webhooks (cert-manager TLS)
- **Idempotent reconciliation**: safe restart, no duplicate resources
- **Orphan cleanup**: detects and removes leftover load balancers
- **Production-ready**: Prometheus metrics, conditions, finalizers,
  CAPI v1beta1 contract compliance

## Quick start

### Prerequisites

- A Kubernetes management cluster with [Cluster API core](https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl) installed
- [cert-manager](https://cert-manager.io/) (only if installing with webhooks)
- An OVH Public Cloud project with API credentials — see the
  [credentials guide](docs/ovh-credentials-guide.md)

### Install (Helm)

```bash
helm install capiovh \
  oci://ghcr.io/rancher-sandbox/charts/cluster-api-provider-ovhcloud \
  --version 0.1.0 \
  --namespace capiovh-system --create-namespace \
  --set webhooks.enabled=true \
  --set webhooks.certManager.enabled=true
```

### Install (manifest)

```bash
kubectl apply -f https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/download/v0.1.1/infrastructure-components.yaml
```

### Provision a cluster

```bash
# 1. Create OVH credentials secret
kubectl create namespace demo
kubectl -n demo create secret generic ovh-credentials \
  --from-literal=endpoint=ovh-eu \
  --from-literal=applicationKey=<AK> \
  --from-literal=applicationSecret=<AS> \
  --from-literal=consumerKey=<CK>

# 2. Generate and apply a Cluster
export OVH_SERVICE_NAME=<project-id>
export OVH_REGION=EU-WEST-PAR
export OVH_SSH_KEY=my-key
clusterctl generate cluster mycluster \
  --from https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/download/v0.1.1/cluster-template-kubeadm.yaml \
  --kubernetes-version v1.31.0 \
  --target-namespace demo | kubectl apply -f -
```

## Architecture

A high-level diagram and reconciliation flow is in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

```
                    Management cluster (CAPI core + CAPIOVH)
                              |
                              | OVH REST API (HMAC-signed)
                              v
                    +---------------------+
                    |   OVH Public Cloud  |
                    +---------------------+
                              |
                              v
                    +---------------------+
                    |  Workload cluster   |
                    |  (Kubernetes)       |
                    +---------------------+
```

## CRDs

| CRD | Purpose |
|-----|---------|
| `OVHCluster` | Cluster-level: project, region, network, LB, optional floating IP |
| `OVHMachine` | Machine-level: instance flavor, image, SSH key, optional volumes |
| `OVHMachineTemplate` | Template referenced by ControlPlane / MachineDeployment |
| `OVHClusterTemplate` | Template referenced by ClusterClass |

## Documentation

- [Architecture](docs/ARCHITECTURE.md) — design overview, reconciliation flows, OVH API quirks
- [OVH credentials guide](docs/ovh-credentials-guide.md) — how to obtain a properly scoped Consumer Key
- [BYOI guide](docs/byoi-guide.md) — uploading custom images (openSUSE, SLES, ...) via Glance
- [Operations](docs/operations.md) — install, monitor, upgrade, uninstall in production
- [Testing](docs/TESTING.md) — unit, envtest, and end-to-end tests
- [Troubleshooting](docs/TROUBLESHOOTING.md) — common issues and fixes
- [Development](docs/DEVELOPMENT.md) — dev environment setup, build, test
- [Release process](docs/RELEASE.md) — how releases are cut
- [Cluster templates](templates/README.md) — variable reference for each template

## Contributing

Pull requests welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

For security issues, see [SECURITY.md](SECURITY.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).
