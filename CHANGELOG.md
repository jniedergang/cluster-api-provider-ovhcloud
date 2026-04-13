# Changelog

All notable changes to cluster-api-provider-ovhcloud are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [v0.1.1] - 2026-04-13

### Fixed

- **Helm chart**: bundled CRDs now carry the CAPI provider labels
  (`cluster.x-k8s.io/provider=infrastructure-ovhcloud`,
  `cluster.x-k8s.io/v1beta1=v1alpha1`). Without these, CAPI core did not
  discover the provider after `helm install` and `OVHCluster` never got
  an `OwnerRef` from the Cluster controller, hanging reconciliation in
  "Waiting for Cluster Controller". Users on v0.1.0 should upgrade.
- **E2E test suite**: the idempotency suite applied a standalone
  `OVHCluster` with no parent `Cluster`, which could not progress even
  with a correctly-labelled chart. It now wraps the resource in a
  `Cluster` + credential `Secret` like the lifecycle suite.

## [v0.1.0] - 2026-04-13

Initial release.

### Added

- **CRDs** (CAPI v1beta1 contract):
  - `OVHCluster`, `OVHMachine`, `OVHMachineTemplate`, `OVHClusterTemplate`
  - All implement standard CAPI status (`ready`, `failureReason`,
    `failureMessage`, `conditions`, `addresses`)
  - Validating webhooks (admission.CustomValidator) for OVHCluster and
    OVHMachine, deployed via cert-manager
- **OVH client library** (`pkg/ovh`):
  - Full CRUD for instances, flavors, images, SSH keys, vRack networks,
    subnets, Octavia load balancers (LBs), listeners, pools, members,
    floating IPs, block storage volumes
  - Idempotent LB creation (find-by-name before POST)
  - Polling after async POST (Octavia returns task descriptor, not LB)
  - BYOI image support: `GetImageByName` searches both `/image` (public
    catalog) and `/snapshot` (custom-uploaded) transparently
  - Retry with exponential backoff on transient errors (429, 5xx, network)
- **Cluster controller**:
  - Reconciles network + subnet + LB + listener + pool
  - Optional floating IP for public API server endpoint
  - Idempotent and safe to restart
  - Cleans up orphan LBs (by name prefix) on cluster deletion
- **Machine controller**:
  - Resolves flavor and image names to IDs
  - Creates instance with cloud-init userData from CAPI bootstrap secret
  - Polls BUILD -> ACTIVE
  - Sets providerID and addresses
  - Initializes workload node (sets providerID, removes uninitialized
    taint) since OVH has no managed CCM
  - Removes etcd member from workload cluster on CP node deletion (RKE2)
- **Distribution**:
  - Multi-arch container images (linux/amd64, linux/arm64) on ghcr.io
  - Helm chart on ghcr.io OCI registry
  - Standalone manifest (`infrastructure-components.yaml`) for clusterctl
  - 4 cluster templates: RKE2, RKE2 + floating IP, kubeadm, ClusterClass
- **CI/CD**: GitHub Actions for lint, test, build, release
- **Documentation**: Architecture, operations, troubleshooting,
  development, release process, BYOI, OVH credentials guide

### Tested

- Live end-to-end on OVH `EU-WEST-PAR` region: Cluster + OVHCluster +
  OVHMachine, full lifecycle including cleanup
- Webhook validation deployed via cert-manager on RKE2 management cluster
- Helm chart install with webhooks enabled

[Unreleased]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/compare/v0.1.1...HEAD
[v0.1.1]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.1.1
[v0.1.0]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.1.0
