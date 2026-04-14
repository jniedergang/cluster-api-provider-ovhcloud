# Changelog

All notable changes to cluster-api-provider-ovhcloud are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [v0.2.0] - 2026-04-14

### Added

- **Rancher Turtles integration**: ship
  `templates/capiprovider-ovhcloud.yaml` (CR
  `turtles-capi.cattle.io/v1alpha1/CAPIProvider`) so users can install,
  upgrade and monitor CAPIOVH from Rancher Manager + Rancher Turtles.
  The manifest is also uploaded as a release asset.
- **Fleet / CAAPF addon management**: documented pattern for delivering
  CNI tuning and Helm addons to workload clusters via the
  [Cluster API Addon Provider for Fleet](https://github.com/rancher/cluster-api-addon-provider-fleet):
  - `manifests/caapf-provider.yaml` — installs CAAPF as a `CAPIProvider`
    of type `addon`
  - `templates/addons/calico-helmchartconfig.yaml` — example override
    for the Canal CNI (MTU for OVH vRack, IP pool sizing)
  - `templates/addons/cilium-helmchartconfig.yaml` — example Cilium
    config with kube-proxy replacement
  - `templates/addons/README.md` — repository layout for the external
    Fleet addon repo
  - `docs/fleet-addons.md` — end-to-end architecture + how-to guide
- **`metadata.yaml`**: extended with `v0.2` release series so
  `clusterctl` accepts this minor.
- **Rancher import helper**: new `scripts/import-to-rancher.sh` performs
  the `cluster.management.cattle.io` discovery, applies the registration
  manifest, and idempotently patches the `cattle-cluster-agent`
  Deployment to mount the `cattle-system/serverca` ConfigMap. Required
  when Rancher uses STRICT_VERIFY=true with a custom or LE-issued cert.
- **`rancherServerCA` ClusterClass topology variable**: when set, the
  bundled ClusterClass writes
  `/var/lib/rancher/rke2/server/manifests/capiovh-rancher-serverca.yaml`
  on each CP node. RKE2's auto-apply mechanism then creates the
  `cattle-system` namespace and the `serverca` ConfigMap during server
  startup, so step 1 of the import helper has its data ready.

### Fixed

End-to-end live validation on OVH `EU-WEST-PAR` uncovered a series of
bugs across DNS, FIP, providerID linkage and Calico routing that all
land in this release. Every cluster created with v0.1.x is functional
but several stuck-state failure modes are now eliminated:

- **DNS bootstrap on private vRack**: OVH vRack DHCP only hands out a
  default route, no DNS server. The previous `preRKE2Commands` snippet
  wrote a `systemd-resolved` drop-in then restarted the service, racing
  with the immediately-following `curl get.rke2.io | sh`. The script
  could resolve `get.rke2.io` but then fail on `github.com`, leaving
  RKE2 never installed. Now we replace `/etc/resolv.conf` with a static
  file pointing at `1.1.1.1`/`8.8.8.8`/`9.9.9.9` and synchronously poll
  until `getent hosts github.com` resolves before continuing.
- **`Machine` ↔ `Node` linkage** (`MachineDeployment` stuck in
  `ScalingUp`): RKE2 registers nodes with `providerID=rke2://<hostname>`
  by default, which never matches the OVHMachine's
  `ovhcloud://<region>/<uuid>`. CAPI cannot link them, so MD reports
  Unavailable and `MachineHealthCheck` cannot remediate. Fixed by
  injecting a kubelet `--provider-id` config drop-in that combines the
  cluster region (written to `/etc/capiovh/region` via a ClusterClass
  JSONPatch) with the instance UUID fetched from OVH OpenStack metadata.
  The previously-orphaned `util.InitializeWorkloadNode` helper is also
  now wired up as a backup in the OVHMachine reconciler.
- **Calico SNAT skipped due to CIDR overlap**: the default RKE2 pod
  CIDR `10.42.0.0/16` overlapped the default `subnetCIDR`
  `10.42.0.0/24`. Calico saw node IPs as inside its IP pool and skipped
  `natOutgoing`, so pods sending to ClusterIPs leaked out instances with
  pod source IPs and were dropped by OVH neutron port-security. The
  ClusterClass now writes
  `/etc/rancher/rke2/config.yaml.d/10-cidrs.yaml` with `cluster-cidr:
  10.244.0.0/16` and `service-cidr: 10.96.0.0/16` so they cannot
  overlap with the default vRack subnet.
- **Floating IP rediscovery** after `CreateLoadBalancerFloatingIP`:
  OVH returns a transient placeholder ID; the actually-allocated FIP
  gets a different ID under the LB's `.floatingIp.id`. Storing the
  placeholder caused the controller to spin forever on
  `Waiting for floating IP to be allocated` and the
  `ControlPlaneEndpoint` was never set. Fixed by re-fetching the LB
  (or listing FIPs by `associatedEntity`) immediately after creation.
- **Floating IP cleanup**: OVH's `DELETE /floatingip/{id}` returns 200
  but is asynchronous and silently detaches (rather than removes) when
  the LB is in `PENDING_DELETE`. `OVHCluster.ReconcileDelete` now
  captures all FIPs associated with the cluster's LB BEFORE deleting
  the LB, then `Get`s after each `Delete` and requeues until the
  resource is truly gone.
- **Load balancer cleanup idempotency**: `DeleteLoadBalancer` now
  treats 409 `Invalid state PENDING_DELETE` / `PENDING_UPDATE` as
  success. New helper `IsAlreadyDeleting` in `pkg/ovh/errors.go`.

### Deferred to v0.3.0

- LoadBalancer `Protocol` refactor to `corev1.Protocol`. Rationale in
  [docs/RELEASE.md](docs/RELEASE.md): breaking change needing a
  v1alpha1↔v1alpha2 conversion webhook, and `corev1.Protocol` drops
  the HTTP value the current custom enum exposes. Will land with the
  v1alpha2 API bump.

## [v0.1.2] - 2026-04-13

### Added

- **Observability**:
  - `NetworkPolicy` restricting ingress on the metrics endpoint to
    namespaces labelled `metrics: enabled` (Helm value
    `networkPolicy.enabled`, kustomize overlay `config/network-policy/`)
  - Prometheus `ServiceMonitor` + metrics `Service` on port 8080
    (Helm value `metrics.serviceMonitor.enabled`, kustomize overlay
    `config/prometheus/`)
  - Six new metrics:
    - `capiovh_node_init_duration_seconds` (workload node init)
    - `capiovh_etcd_member_removal_duration_seconds` (CP deletion)
    - `capiovh_bootstrap_wait_duration_seconds` (BUILD → ACTIVE)
    - `capiovh_lb_poll_duration_seconds` (async LB POST polling)
    - `capiovh_ovh_api_requests_total{endpoint,outcome}`
    - `capiovh_ovh_api_request_duration_seconds{endpoint}`
  - Pre-built Grafana dashboard (`config/grafana/capiovh-dashboard.json`,
    UID `capiovh-overview`), 21 panels across 5 rows
  - Documentation in `docs/operations.md` (Monitoring, NetworkPolicy,
    Observability via Grafana sections)

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

[Unreleased]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/compare/v0.2.0...HEAD
[v0.2.0]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.2.0
[v0.1.2]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.1.2
[v0.1.1]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.1.1
[v0.1.0]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.1.0
