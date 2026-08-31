# cluster-api-provider-ovhcloud

[![lint](https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/actions/workflows/lint.yml/badge.svg)](https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/actions/workflows/lint.yml)
[![test](https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/actions/workflows/test.yml/badge.svg)](https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/actions/workflows/test.yml)
[![e2e](https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/actions/workflows/e2e.yml/badge.svg)](https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/actions/workflows/e2e.yml)
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
  (`ovhcloud-rke2` and `ovhcloud-kubeadm`)
- **MachineHealthCheck**: auto-created from ClusterClass for CP + workers
- **Failure domains**: auto-discovered per region via OVH API
- **CAPI adopt**: zero-downtime migration from existing OVH-managed
  Kubernetes clusters (see [docs/operations.md](docs/operations.md))
- **Addons**: Calico, Cilium, OpenStack CCM, Cinder CSI, cluster
  autoscaler (`templates/addons/`)
- **Webhook validation**: optional admission webhooks (cert-manager TLS)
- **Idempotent reconciliation**: safe restart, no duplicate resources
- **Orphan cleanup**: detects and removes leftover load balancers and FIPs
- **Production-ready**: Prometheus metrics, conditions, finalizers,
  CAPI v1beta2 contract compliance
- **Live E2E CI**: weekly + on-demand workflow against a real OVH
  project (see [docs/TESTING.md](docs/TESTING.md))

## Quick start

For the full walkthrough (~15 min from zero to Ready nodes), see
[docs/quickstart.md](docs/quickstart.md). Condensed version below.

### Prerequisites

- A Kubernetes management cluster with [Cluster API core](https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl) v1.11 or newer installed (the provider targets the CAPI v1beta2 contract)
- [cert-manager](https://cert-manager.io/) (only if installing with webhooks)
- An OVH Public Cloud project with API credentials (see the
  [credentials guide](docs/ovh-credentials-guide.md))

### Install (Helm)

```bash
helm install capiovh \
  oci://ghcr.io/rancher-sandbox/charts/cluster-api-provider-ovhcloud \
  --namespace capiovh-system --create-namespace \
  --set webhooks.enabled=true \
  --set webhooks.certManager.enabled=true
```

(Pin `--version` to a specific tag for reproducible installs, see the
[releases page](https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases).)

### Install (manifest)

```bash
LATEST=$(curl -fsS https://api.github.com/repos/rancher-sandbox/cluster-api-provider-ovhcloud/releases/latest | jq -r .tag_name)
kubectl apply -f "https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/download/${LATEST}/infrastructure-components.yaml"
```

### Provision a cluster

```bash
# 1. Create OVH credentials secret. NB: the SSH key must be registered
# via the OVH native API (POST /cloud/project/{sn}/sshkey), not via
# `openstack keypair create` (see docs/ovh-credentials-guide.md).
kubectl create namespace demo
kubectl -n demo create secret generic ovh-credentials \
  --from-literal=endpoint=ovh-eu \
  --from-literal=applicationKey=<AK> \
  --from-literal=applicationSecret=<AS> \
  --from-literal=consumerKey=<CK>

# 2. Generate and apply a Cluster from the latest release templates
export OVH_SERVICE_NAME=<project-id>
export OVH_REGION=EU-WEST-PAR
export OVH_SSH_KEY=my-key
LATEST=$(curl -fsS https://api.github.com/repos/rancher-sandbox/cluster-api-provider-ovhcloud/releases/latest | jq -r .tag_name)
clusterctl generate cluster mycluster \
  --from "https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/download/${LATEST}/cluster-template-kubeadm.yaml" \
  --kubernetes-version v1.31.0 \
  --target-namespace demo | kubectl apply -f -
```

A topology-based variant using `ClusterClass ovhcloud-rke2` lives at
[`templates/clusterclass/rke2/`](templates/clusterclass/rke2/), see
[docs/quickstart.md](docs/quickstart.md) for the full walkthrough.

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
| `OVHMachinePool` | (CRD only, reconciler not implemented yet) |

## Documentation

- [Quickstart](docs/quickstart.md) (full walkthrough from zero to a Ready RKE2 cluster)
- [Architecture](docs/ARCHITECTURE.md) (design overview, reconciliation flows, OVH API quirks)
- [OVH credentials guide](docs/ovh-credentials-guide.md) (how to obtain a properly scoped Consumer Key)
- [BYOI guide](docs/byoi-guide.md) (uploading custom images like openSUSE or SLES via Glance)
- [Operations](docs/operations.md) (install, monitor, upgrade, uninstall in production)
- [Fleet / CAAPF addons](docs/fleet-addons.md) (deliver CNI tuning and other Helm addons via Fleet)
- [Testing](docs/TESTING.md) (unit, envtest, and end-to-end tests)
- [Troubleshooting](docs/TROUBLESHOOTING.md) (common issues and fixes)
- [Development](docs/DEVELOPMENT.md) (dev environment setup, build, test)
- [Release process](docs/RELEASE.md) (how releases are cut)
- [Cluster templates](templates/README.md) (variable reference for each template)

## Contributing

Pull requests welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

For security issues, see [SECURITY.md](SECURITY.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).
