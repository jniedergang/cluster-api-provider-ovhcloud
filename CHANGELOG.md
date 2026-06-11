# Changelog

All notable changes to cluster-api-provider-ovhcloud are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed (CI / test infrastructure)

- E2E workflow now runs reliably against the live OVH project. Fifteen
  fixes collected during the first end-to-end green run: corrected
  invalid `actions/upload-artifact` SHA pin; bridged `podman` build to
  `kind` via `docker load`; routed image through `docker-archive`
  rather than OCI archive; overrode the hardcoded upstream image in
  `manager_image_patch.yaml`; enabled the `ClusterTopology` feature
  gate for ClusterClass support; synced the conversion-webhook service
  reference with the kustomize `namePrefix`; defaulted the test region
  to `EU-WEST-PAR` (where the SSH key is registered); pass the full
  OVH credential set to the E2E step; polling loop now survives
  transient kubectl/apiserver errors with a 60 s heartbeat; FIPs in
  `down + null associatedEntity` are no longer counted as leaks (they
  are awaiting OVH async reap); tolerated slow OVH LB provisioning and
  stuck `PENDING_CREATE` LBs; install `kustomize` via `go install`
  instead of the GitHub-hosted script (avoids the anonymous rate
  limit). See `4fc5589`, `62e39ee`, `01c0369`, `cefbbf4`, `d86f8c2`,
  `3891bd5`, `4a72e26`, `d4da731`, `4c568c1`, `b99c6b7`, `3ca82c4`,
  `bcfbd96`, `a003ad1`, `103d24d`, `6ee2643`, `5cb5dab`, `40d007c`.
- New scripts `test/e2e/pre-cleanup.sh` and
  `test/e2e/collect-instance-logs.sh`: wipe leftover OVH resources
  before each run, and SSH into still-running instances on failure to
  grab cloud-init / RKE2 logs.

### Security

- Workflow hardened against secret exfiltration on a public repo:
  bound to a GitHub deployment environment `ovh` requiring manual
  reviewer approval, secrets passed via stdin/files instead of
  `--from-literal=$VAR`, artifact retention reduced to 1 day, branch
  ruleset on `main` blocking force-push and deletion (`82fde50`).
- `.gitignore` now covers `.env` and `.env.*` to prevent accidental
  commit of locally-staged credentials (`63b2892`).

### Added (UI extension)

- Rancher Dashboard extension `ui/pkg/capiovh`: tile + product page
  under "OVH Cloud" with create-cluster button and transition
  messaging. Forked from the CAPI UI extension skeleton with
  `@rancher/shell` v3 and `importTypes` auto-registration. Twenty-plus
  iterative fixes converged on a single working tarball
  (`extensions/capiovh/0.1.0.tgz`) loadable via a Rancher `UIPlugin`
  CR. See the `feat(ui):` / `fix(ui):` series between `9dc8d4d` and
  `c681c10`.

### Added

- **CAPI adopt** — zero-downtime migration of existing OVH-managed
  clusters: `OVHCluster.spec.existingLoadBalancerID`,
  `existingFloatingIPID`, and `OVHMachine.spec.existingInstanceID` let
  the controller adopt pre-existing resources instead of creating new
  ones. The annotation
  `capiovh.cluster.x-k8s.io/adoption-policy: keep` preserves OVH
  resources on `Cluster` deletion so operators can safely roll back
  during the cutover (`6cdd58a`).

### Changed

- Routine Dependabot bumps:
  `actions/checkout` 4.3.1→6.0.3, `actions/setup-go` 5.6.0→6.4.0,
  `actions/upload-artifact` 4.6.2→7.0.1, `helm/kind-action` 1.12.0→1.14.0,
  `sigstore/cosign-installer` 4.1.1→4.1.2, `golangci/golangci-lint-action`
  9.2.0→9.2.1, `docker/login-action` 4.1.0→4.2.0,
  `docker/build-push-action` 7.1.0→7.2.0,
  `docker/setup-qemu-action` 4.0.0→4.1.0,
  `docker/setup-buildx-action` 4.0.0→4.1.0,
  `docker/metadata-action` 6.0.0→6.1.0, `bci/golang` 1.24.5→1.26.3,
  `github.com/prometheus/client_golang` 1.22.0→1.23.2,
  `github.com/go-logr/logr` 1.4.2→1.4.3.

### Documentation

- Documented the E2E workflow, required `ovh` environment secrets,
  pre-cleanup / SSH-collect scripts, and timing knobs in
  `docs/TESTING.md`.
- Six new `docs/TROUBLESHOOTING.md` sections covering quirks
  discovered live: SSH key not found despite `openstack keypair list`
  showing it; region-scoped SSH keys; `vlanId` project-scoped
  collision; network DELETE blocked by `router_interface_distributed`
  port; FIP accumulation hitting the router quota; LB wedged in
  `PENDING_CREATE`; `Provisioned`-but-no-init diagnostic flow.
- New "SSH key pitfall" section in `docs/ovh-credentials-guide.md`
  explaining the OpenStack vs OVH-native two-inventory split and the
  curl recipe to register a key the controller can actually see.

## [v0.5.0] - 2026-04-15

### Added

- **kubeadm ClusterClass** (`templates/clusterclass/kubeadm/`) as a
  topology-based alternative to the RKE2 path.
- **Registry mirror** support in cluster templates for air-gapped or
  private-registry installs.
- **Drain timeout** configurable per MachineDeployment.
- **etcd S3 backup** topology variable for RKE2 control planes.
- **Ignition bootstrap format** (`OVHMachine.spec.bootstrapFormat`)
  in addition to the default cloud-init.

See commit `733adaa` for the full set.

## [v0.4.0] - 2026-04-15

### Added

- **Failure domains auto-discovery**: `OVHCluster.status.failureDomains`
  is populated via `GetRegionInfo()`. Regions with multiple AZs (e.g.
  GRA11) expose them to CAPI for per-machine placement.
- **DNS record creation** (optional): controller creates an A record
  for the LB FIP in an OVH DNS zone when `OVHCluster.spec.dnsConfig`
  is set. Requires `/domain/*` scope on the OVH Consumer Key.
- **Cluster autoscaler addon** manifest under `templates/addons/`.
- **OVHMachinePool CRD** stub (reconciler not implemented yet).
- **E2E CI workflow** scaffold (`.github/workflows/e2e.yml`) — finalized
  in the Unreleased section above.

### Changed

- `OVHMachine.spec.metadata` field removed — OVH native API rejects
  it as an unknown parameter on `POST /cloud/project/{sn}/instance`
  (`5164b4a`).
- Honest test results in `docs/TESTING.md`: features that did not
  actually work were marked as such and removed from the topology
  rather than leaving false ✅ rows (`ca560d0`).

See commits `6db76fe` and `ca560d0` for the full set.

## [v0.3.1] - 2026-04-15

### Fixed

- **Gateway expose idempotence**: `ExposeGateway` treats OVH's 409
  Conflict response (gateway already exposed) as a success rather
  than a hard error. Eliminates ~250 spurious errors per 12 h in the
  controller logs.
- **`disableCloudController` ClusterClass variable**: writes
  `disable-cloud-controller: true` to the RKE2 config on CP and worker
  nodes. Required when deploying the OpenStack CCM (which conflicts
  with RKE2's built-in CCM).
- **CSI test fixes**: chart versions and secret config in
  `templates/addons/cinder-csi-helmchartconfig.yaml` corrected.

See commit `7b3c846`.

## [v0.3.0] - 2026-04-15

### Breaking Changes

- **v1alpha2 API version**: the storage version is now
  `infrastructure.cluster.x-k8s.io/v1alpha2`. A conversion webhook
  serves both `v1alpha1` and `v1alpha2` — existing v1alpha1 resources
  are converted automatically. The only breaking field change is
  `OVHListener.Protocol`: the custom `string` enum (`TCP`, `HTTP`) is
  replaced by `corev1.Protocol` (`TCP`, `UDP`, `SCTP`). `HTTP` is
  removed — no shipped template or controller code ever used it.
  Clusters with `Protocol: HTTP` listeners will fail conversion with a
  clear error message.

### Added

- **MachineHealthCheck in ClusterClass**: the bundled
  `clusterclass-ovhcloud-rke2` now includes `machineHealthCheck` blocks
  for both control-plane and worker MachineDeployments (maxUnhealthy
  34%, nodeStartupTimeout 20m, Ready False/Unknown timeout 5m). CAPI
  automatically creates the corresponding MHC CRs for every Cluster
  using this topology. The three standalone templates already shipped
  MHC resources since v0.2.0.
- **Cinder CSI addon manifest**: `templates/addons/cinder-csi-helmchartconfig.yaml`
  installs the upstream OpenStack Cinder CSI driver on a workload
  cluster via RKE2 HelmChart. Enables PersistentVolume provisioning
  from OVH block storage. Requires OpenStack credentials — see
  `docs/operations.md` for setup.
- **OpenStack CCM addon manifest**: `templates/addons/openstack-ccm-helmchartconfig.yaml`
  installs the upstream OpenStack Cloud Controller Manager. Enables
  `Service type=LoadBalancer` (via Octavia) from workload clusters.
  Note: CAPIOVH's `InitializeWorkloadNode` remains active but is
  idempotent — the CCM becomes the authoritative providerID source
  once deployed.
- **OpenStack credentials documentation** in `docs/operations.md`:
  how to obtain OVH OpenStack credentials and create the `cloud-config`
  Secret for CSI/CCM addons.
- **`metadata.yaml`** extended with the `v0.3` release series.

### Fixed

- **Stale troubleshooting reference**: `docs/TROUBLESHOOTING.md` no
  longer references "Tracked in v0.3.0" for the LB health monitor
  issue (already shipped in v0.2.2).

## [v0.2.3] - 2026-04-15

### Security

- **Supply chain hardening** (parallels CAPHV v0.2.9 / rancher-security#1667).
  Brings CAPIOVH up to the same baseline expected before a Rancher Sandbox
  security scan:
  - **Action SHA pinning**: all 11 GitHub Actions in the four workflows
    (`build.yml`, `lint.yml`, `release.yml`, `test.yml`) are now pinned to
    full 40-char commit SHAs with `# vX.Y.Z` trailing comments. Tag
    references like `@v4` are mutable and would be flagged as a
    tag-hijack risk. Manual maintenance is required when bumping —
    Dependabot's default behaviour is tags, not SHAs.
  - **cosign keyless signing** of every released container image via
    Sigstore OIDC. Verification requires `cosign` v3.0.0+ (v2.x reports
    "no signatures found" on v3-format Sigstore bundles even when valid).
  - **SLSA build provenance** attestation
    (`actions/attest-build-provenance@v4.1.0`) is generated and pushed
    to the registry alongside the image. Verifiable via
    `gh attestation verify oci://... --owner <org>`.
  - **BuildKit SBOM** + max-mode provenance enabled in
    `docker/build-push-action` (`sbom: true`, `provenance: mode=max`).
  - **`hack/`-style hadolint script**:
    `scripts/ci-lint-dockerfiles.sh` — reusable Kubernetes-project
    boilerplate for Dockerfile linting (warning-or-higher fails CI).
  - **Dependabot** config (`.github/dependabot.yml`): weekly bumps for
    `gomod`, `docker`, and `github-actions` ecosystems. The CAPI core
    deps (`cluster-api`, `controller-runtime`, `k8s.io/*`, `etcd`,
    `grpc`) are pinned out and bumped manually together to avoid CAPI
    contract breakage.
  - **Least-privilege workflow permissions**: each workflow now has a
    workflow-level `permissions: contents: read` default and per-job
    overrides only where needed (e.g. `packages: write` for the build
    job, `id-token: write` + `attestations: write` for cosign + SLSA).
    The `release` job has `contents: write` for `gh release create`.
    All other jobs are read-only.
  - `persist-credentials: false` added to every `actions/checkout` step
    to limit the post-checkout credential blast radius.

### Notes

- No code changes — pure CI/CD and policy hardening.
- v0.2.2 image is unsigned; v0.2.3 will be the first cosigned release.
- Dependabot will start opening PRs after the merge — they may include
  tag-style bumps; reject those and apply the SHA pin manually (see
  `CONTRIBUTING.md` → "Updating GitHub Actions").

## [v0.2.2] - 2026-04-15

### Added

- **LB pool health monitor**: every Octavia pool created by CAPIOVH
  (api-server-pool and rke2-register-pool) now gets a TCP health
  monitor attached (delay 5s, timeout 3s, max 2 retries → ~10 s
  failure detection). Without it, the LB kept routing traffic to a
  dead CP after a node failure, dropping API availability to ~52 %
  during a 3-CP failover (v0.2.1 baseline). With the monitor, the
  same scenario reaches **100 % availability** (260 / 260 probes
  succeed) and CAPI rebuilds the failed CP in ~4 m 21 s. Implementation
  is idempotent: the helper finds the existing monitor by name and
  retries on every reconcile to absorb the OVH "pool immutable"
  race that locks the pool ~1 s after creation.
- **RKE2 etcd snapshot lifecycle helper**:
  `scripts/rke2-etcd-snapshot.sh` exposes `list / create / restore`
  via SSH against any control-plane node. Restore is intentionally
  guided (single-CP `--cluster-reset`, then operator deletes the
  other CPs via CAPI) to prevent silent data divergence.
  Default RKE2 schedule (`0 */5 * * *`, 5-snapshot retention,
  on-disk at `/var/lib/rancher/rke2/server/db/snapshots/`) is
  documented in [docs/operations.md](docs/operations.md), along
  with a fallback procedure (privileged hostPID pod with
  `chroot /host`) for OVH base images that don't grant sudo
  NOPASSWD despite cloud-init.

### Fixed

- **FIP cleanup convergence on cluster delete**: the OVH floating-IP
  DELETE endpoint returns 200 immediately but the resource lingers
  for several minutes with `status: down` and
  `associatedEntity: null`. v0.2.1 retried the DELETE in a tight
  reconcile loop, never converged, and required a manual
  `ovh DELETE /floatingip/{id}` call to unblock cluster teardown.
  Now the controller treats `detached + down` as already-deleted
  (OVH will reap async), distinguishing it from a still-attached
  FIP that genuinely needs another delete attempt. Cluster delete
  now converges without manual intervention in the common case.

## [v0.2.1] - 2026-04-14

### Added

- **Multi-cluster vRack isolation**: new `vlanID` topology variable
  (and `OVHClusterSpec.NetworkConfig.VlanID` field, range 0-4094). OVH
  allows only one private network per VLAN ID per project; CAPIOVH
  used to always create networks with vlanId=0, silently blocking any
  second cluster in the same project. Each cluster must now declare a
  distinct vlanID when sharing a project.
- **Production readiness validation matrix** in
  [docs/TESTING.md](docs/TESTING.md) tracking 17 manual scenarios
  (cluster lifecycle, scale, upgrade, HA, multi-cluster, etc.).
- **`scripts/import-to-rancher.sh`** companion + `rancherServerCA`
  topology variable already shipped in v0.2.0; this release confirms
  the live UI workflow is wired end-to-end.
- **`test/e2e/run-validation-tests.sh`**: 16 negative webhook + CRD
  validation cases plus 2 positive sanity specs.

### Fixed

- **DNS bootstrap race condition**: previous `preRKE2Commands` restarted
  systemd-resolved after replacing `/etc/resolv.conf`, which races with
  the immediately-following `curl get.rke2.io | sh`. Symptoms: the wait
  loop completes in <1s but `curl github.com` then fails with
  `Could not resolve host`, blocking RKE2 install entirely. Live
  observed on capi-ovh-2 v0.2.1-rc1. Fix: drop the resolved restart
  (the static `/etc/resolv.conf` is enough — libresolv reads it on
  every query) and switch the wait probe from `getent` (NSS, hits
  systemd-resolved stub) to a `python3 socket.gethostbyname` call
  (libresolv directly, same path as curl).

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

[Unreleased]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/compare/v0.3.0...HEAD
[v0.3.0]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.3.0
[v0.2.3]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.2.3
[v0.2.2]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.2.2
[v0.2.1]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.2.1
[v0.2.0]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.2.0
[v0.1.2]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.1.2
[v0.1.1]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.1.1
[v0.1.0]: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/tag/v0.1.0
