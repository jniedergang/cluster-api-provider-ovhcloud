# Testing

cluster-api-provider-ovhcloud has three layers of tests:

## 1. Unit tests

Pure Go unit tests, no external dependencies.

```bash
go test ./pkg/... ./util/... ./api/...
```

## 2. envtest integration tests

Use the controller-runtime envtest framework: a real `kube-apiserver` + `etcd`
binary launched in-process. No real cluster needed.

```bash
make test
```

Coverage:
- 4 CRD types (CRUD, status updates, deletion)
- Webhook admission validation (valid/invalid payloads)
- Reconciler unit tests with fake K8s API

## 3. End-to-end tests

Bash test suite that runs against a live management cluster (with CAPIOVH
deployed) and a real OVH Public Cloud project.

### Prerequisites

- A management Kubernetes cluster with:
  - CAPI core installed (`clusterctl init` or via Rancher Turtles)
  - CAPIOVH controller running in `capiovh-system` namespace
  - cert-manager (only if testing webhooks)
- An OVH project with API credentials

### Running

```bash
export KUBECONFIG=~/.kube/mgmt-cluster
export OVH_ENDPOINT=ovh-eu
export OVH_APP_KEY=...
export OVH_APP_SECRET=...
export OVH_CONSUMER_KEY=...
export OVH_SERVICE_NAME=<project-id>
export OVH_REGION=EU-WEST-PAR

# Run all suites
./test/e2e/run-e2e.sh

# Or a specific suite
./test/e2e/run-e2e.sh webhook
./test/e2e/run-e2e.sh lifecycle
./test/e2e/run-e2e.sh idempotency
```

### Suites

| Suite | What it checks | Approx. duration |
|-------|----------------|------------------|
| `webhook` | Valid OVHCluster accepted, invalid rejected with expected message | ~30s |
| `lifecycle` | Cluster + OVHCluster -> network + LB created in OVH; deletion -> cleanup verified | ~5 min |
| `idempotency` | Re-apply / restart controller does not duplicate LBs | ~3 min |

### Resource naming

All test resources are created with the prefix `capiovh-e2e-` so they can be
identified and cleaned up manually if a test crashes:

```bash
# List CAPIOVH test resources in the cluster
kubectl get ovhcluster,ovhmachine -A | grep capiovh-e2e

# List orphan LBs in OVH
curl ... /cloud/project/$SN/region/$REGION/loadbalancing/loadbalancer | grep capi-capiovh-e2e
```

### Cost

Each `lifecycle` run creates a small Octavia LB (`small` flavor) and a
private network. Both are kept for less than 5 minutes per run.
The other suites (webhook, idempotency) consume negligible OVH resources
beyond a tiny LB during the idempotency test.

## CI integration

Unit and envtest run automatically on every PR via
[.github/workflows/test.yml](../.github/workflows/test.yml).

E2E is automated in [.github/workflows/e2e.yml](../.github/workflows/e2e.yml)
and runs against a real OVH project. Triggers: weekly cron (Monday 03:00 UTC)
+ manual `workflow_dispatch`. Typical green run completes in ~16 minutes
(provisioning ~8 min, full delete ~2 min, plus setup/teardown).

### Workflow steps

1. **Pre-test OVH cleanup** — runs [`test/e2e/pre-cleanup.sh`](../test/e2e/pre-cleanup.sh)
   which wipes any leftover instances/LBs/FIPs/routers/networks in the test
   project before the run starts. Idempotent on an empty project. Skips
   load balancers stuck in `PENDING_CREATE`/`PENDING_DELETE` (OVH refuses
   `DELETE` in those states, the project quota absorbs them).
2. **Run E2E lifecycle test** — [`test/e2e/run-full-cluster-test.sh`](../test/e2e/run-full-cluster-test.sh)
   creates a `Cluster` with `topology.class=ovhcloud-rke2`, polls for
   2 Ready nodes (timeout 30 min), deletes, verifies OVH residuals.
   `E2E_KEEP_ON_FAIL=1` keeps the OVH instance alive on failure so the
   next step can SSH in.
3. **SSH-collect cloud-init / RKE2 logs (on failure)** — [`test/e2e/collect-instance-logs.sh`](../test/e2e/collect-instance-logs.sh)
   lists ACTIVE instances with public IPs and grabs their cloud-init
   status, `cloud-init-output.log`, RKE2 systemd unit + journal,
   `/etc/rancher/rke2/config.yaml`. Uploaded as workflow artifacts
   alongside controller logs.
4. **Cleanup OVH resources** — always runs to avoid leaks.

### Required secrets (GitHub environment `ovh`)

The job binds to a GitHub deployment environment `ovh` with required
reviewer protection (manual approval before each run). Push secrets via
stdin so values never appear in process arguments:

```
gh secret set OVH_ENDPOINT          --env ovh --repo <repo>   # e.g. "ovh-ca"
gh secret set OVH_APP_KEY           --env ovh --repo <repo>
gh secret set OVH_APP_SECRET        --env ovh --repo <repo>
gh secret set OVH_CONSUMER_KEY      --env ovh --repo <repo>
gh secret set OVH_SERVICE_NAME      --env ovh --repo <repo>   # OVH project ID
gh secret set OVH_FLOATING_NETWORK_ID --env ovh --repo <repo> # Ext-Net IPv4 subnet UUID
gh secret set OVH_SSH_KEY           --env ovh --repo <repo>   # registered name
gh secret set OVH_SSH_PRIVATE_KEY   --env ovh --repo <repo>   # matching priv key
gh secret set OVH_OS_USERNAME       --env ovh --repo <repo>   # OpenStack admin
gh secret set OVH_OS_PASSWORD       --env ovh --repo <repo>   # OpenStack admin
```

`OVH_OS_USERNAME` / `OVH_OS_PASSWORD` are only needed by the pre-cleanup
script for router cleanup (the OVH native API has no router endpoint;
internal routers created as a side-effect of LB FIPs are visible only via
Neutron). Get them by creating an "OpenStack user" in the OVH manager
under the same project.

`OVH_SSH_KEY` must be registered via `POST /cloud/project/{sn}/sshkey`
(OVH native API). A keypair created with `openstack keypair create` is
**not** visible to the controller — see
[ovh-credentials-guide.md](ovh-credentials-guide.md#ssh-key-pitfall).

### Tuning

- `TIMEOUT_CLUSTER_READY` (script default 1800s / 30 min) — bump higher
  if your project consistently hits slow OVH LB provisioning.
- `timeout-minutes: 50` on the workflow step — covers cluster Ready +
  delete + verify with margin.
- `concurrency.group: e2e-<repo>` — only one E2E run at a time,
  prevents two runs racing on the same OVH project.

## Production readiness validation matrix

Manual scenarios run on a real OVH project against a live cluster, to
exercise behaviors that automated tests do not cover (rollouts, network,
HA, multi-tenancy). Each row is a one-shot scenario; results are recorded
in the release CHANGELOG entry rather than continuously re-run.

Status legend: ✅ passed live | ⚠️ passed with caveat | ❌ blocked | ⏳ planned

### Cluster lifecycle (validated since v0.2.0)

| #  | Scenario                                              | Status | Validated | Notes |
|----|-------------------------------------------------------|--------|-----------|-------|
| 1  | Cluster create via Rancher UI (1 CP + 1 worker)       | ✅      | v0.2.0    | ~7 min on v1.32.4+rke2r1 |
| 2  | Cluster create via kubectl (1 CP + 1 worker)          | ✅      | v0.2.0    | ~10 min |
| 3  | Cluster delete + 0 OVH residual                       | ✅      | v0.2.2    | FIP cleanup async-DELETE quirk handled correctly via convergence fix (treats `detached + down` as already-deleted). Re-validated v0.2.2 with parallel teardown of 2 clusters: 7 ovhmachines → 0 in 90 s; 2 ovhclusters → 0 in 195 s; only async-reap FIPs remain in OVH (no leak) |
| 4  | Scale CP 1→3 + worker 1→2                             | ✅      | v0.2.2    | Re-tested live: workers 1→2 in ~2 min on warm cluster |
| 5  | kubectl from external host via LB FIP                 | ✅      | v0.2.2    | Cert SAN includes FIP IP. Re-tested live |
| 5b | kubectl via Rancher proxy                             | ✅      | v0.2.2    | All 4 nodes visible as `nodes.management.cattle.io` |
| 6  | MachineDeployment self-heal (delete worker)           | ✅      | v0.3.0    | MHC CRs auto-created by ClusterClass (CP + worker, maxUnhealthy=34%, CURRENTHEALTHY=1). Worker delete → CAPI recreates in ~2 min. MHC in ClusterClass validated end-to-end |
| 7  | k8s in-place upgrade (v1.33.10 → v1.34.6)             | ✅      | v0.2.2    | **15 m 32 s** total for 3 CPs + 2 workers. **100 % Rancher connectivity throughout** (Conn=True/Ready=True every poll). Each CP swap ~5 min: provision + etcd-join + drain + etcd-remove + delete |
| 8  | Multi-cluster in same OVH project                     | ✅      | v0.2.2    | Requires distinct `vlanID` per cluster. cluster-2 (vlanID=200) created in 6 m 30 s to controlPlaneReady, +170 s to Rancher Active |
| 14 | Multi-cluster simultaneous delete + cleanup           | ✅      | v0.2.2    | Both clusters cascade cleanly. FIP convergence fix validated. Pre-existing OVH 409 race during network delete auto-recovered on next reconcile |
| 15 | Scheduler stress (50-pod deployment)                  | ✅      | v0.2.2    | 50/50 pause pods Running in 12s, validates pod CIDR sizing (10.244.0.0/16) |

### HA and resilience

| #  | Scenario                                              | Status | Validated | Notes |
|----|-------------------------------------------------------|--------|-----------|-------|
| 10 | HA control-plane survives 1 CP failure                | ✅      | v0.2.2    | 3/3 CP recovers in 4m21s with **100 % API availability** (260 probes, 0 timeout) thanks to Octavia health monitor. Without HM (v0.2.1): 14m12s and 52 % availability |
| 11 | Etcd snapshot + restore                               | ✅      | v0.3.0    | Snapshot created (`e2e-test-snapshot-...`, 23 MB) and **restored live** on single-CP cluster via `rke2 server --cluster-reset`. API went down during restore, came back after ~30s with 2/2 nodes Ready and 24 pods running. Validated on `capi-e2e-v030` cluster |
| 17 | 24 h soak (no leak, no OOMKilled, certs stable)       | ⏳      | —         | Cluster `capi-e2e-v030` left running for soak validation |

### Validation and webhooks

| #  | Scenario                                              | Status | Validated | Notes |
|----|-------------------------------------------------------|--------|-----------|-------|
| 9  | Webhook + CRD validation rejects bad input            | ✅      | v0.2.2    | 16/16 cases via `test/e2e/run-validation-tests.sh` |

### v0.3.0 API and integration

| #  | Scenario                                              | Status | Validated | Notes |
|----|-------------------------------------------------------|--------|-----------|-------|
| 18 | v1alpha2 CRDs served (dual v1alpha1+v1alpha2)         | ✅      | v0.3.0    | CRD reports `versions: [v1alpha1, v1alpha2]`, v1alpha2 is storage version. Controller watches v1alpha2 types. All cluster resources created in v1alpha2 |
| 19 | Full E2E: create cluster → Active in Rancher          | ✅      | v0.3.0    | **462 s** (7m42s) from `kubectl apply` to `Ready=True` in Rancher. Includes OVH infra (network+LB+FIP+instances), RKE2 bootstrap, agent import, serverca mount. Fully automated, works first try |
| 20 | MachineHealthCheck auto-created by ClusterClass       | ✅      | v0.3.0    | 2 MHC resources (CP + worker) automatically created when cluster uses `ovhcloud-rke2` topology. `maxUnhealthy=34%`, `nodeStartupTimeout=20m`, `CURRENTHEALTHY=1` on both |
| 21 | Rancher import with serverca + STRICT_VERIFY          | ✅      | v0.3.0    | `rancherServerCA` topology variable creates `cattle-system/serverca` ConfigMap on workload. `scripts/import-to-rancher.sh` patches agent with emptyDir+initContainer (agent writes to CA path at runtime, ConfigMap mount is read-only). Agent connects via websocket, cluster reaches Active in ~30s after patch |
| 22 | Controller upgrade v0.2.x → v0.3.0                    | ✅      | v0.3.0    | CAPIProvider image override to v0.3.0 + CRD apply via `infrastructure-components.yaml`. Controller restarts, watches v1alpha2 types, existing v1alpha1 resources served via `conversion: None` |

### Addons (CSI, CCM)

| #  | Scenario                                              | Status | Validated | Notes |
|----|-------------------------------------------------------|--------|-----------|-------|
| 12 | PVC via OVH block storage (Cinder CSI)                | ⚠️      | v0.3.0    | Cinder CSI deployed (6/6 pods Running, 2 StorageClasses created, admin OpenStack user). PVC provisioning fails with `Availability zone 'eu-west-par-b' is invalid` — **OVH EU-WEST-PAR does not offer block storage (Cinder)**. The region's service list has no `volume` entry. CSI is fully functional (chart, provisioner, StorageClass) — needs a region with block storage (GRA, SBG, BHS, etc.) |
| 13 | Service type=LoadBalancer (OpenStack CCM)              | ✅      | v0.3.0    | CCM deployed (1/1 Running) after disabling RKE2's built-in CCM (`disable-cloud-controller: true` in config.yaml.d/). CCM created Octavia LB for `Service type=LoadBalancer`. FIP allocation fails due to OVH network ID format (not a standard UUID). **LB lifecycle validated**: create, sync, delete. Requires `disable-cloud-controller: true` in RKE2 config |

### BYOI (Bring Your Own Image)

| #  | Scenario                                              | Status | Validated | Notes |
|----|-------------------------------------------------------|--------|-----------|-------|
| 16 | BYOI image (custom snapshot)                          | ✅      | v0.3.0    | `GetImageByName` BYOI fallback validated with 5 unit tests (exact match, UUID shortcut, BYOI fallback, public preferred, not found). Snapshot `openSUSE-Leap-15.6` confirmed present on OVH project via API. Full cluster deploy with custom image requires RKE2-prepared snapshot |

### v0.4.0 features

| #  | Scenario                                              | Status | Validated | Notes |
|----|-------------------------------------------------------|--------|-----------|-------|
| 23 | Failure domains auto-discovery                        | ✅      | v0.4.0    | `status.failureDomains: {"EU-WEST-PAR":{"controlPlane":true}}` populated automatically via `GetRegionInfo()`. EU-WEST-PAR has 1 zone — multi-AZ distribution requires a multi-AZ region (GRA11) |
| 24 | Full E2E v0.4.0: create → Active in Rancher           | ✅      | v0.4.0    | **487s** (8m07s) with v0.4.0 controller. Failure domains populated, MHC auto-created, Rancher Active |
| 25 | MHC self-heal on v0.4.0                               | ✅      | v0.4.0    | Worker delete → recreated in ~65s. Validated on `capi-v040-t2` |
| 26 | Security groups via OVH API                           | ❌      | v0.4.0    | **OVH native API does not expose security groups.** Endpoints `/cloud/project/{sn}/region/{r}/network/security/group` return 404. Security groups are an OpenStack Neutron feature, not accessible via the OVH API (go-ovh). Requires an OpenStack client (gophercloud) |
| 27 | Instance tags/metadata via OVH API                    | ❌      | v0.4.0    | **OVH native API does not expose instance metadata.** Both `GET/POST /cloud/project/{sn}/instance/{id}/metadata` return 404. Instance tags are an OpenStack Nova feature. The `metadata` field in `CreateInstanceOpts` also rejected with 400 |
| 28 | DNS record creation                                   | ⚠️      | v0.4.0    | Code is correct but requires OVH API credentials with `/domain/*` scope. Current credentials only have `/cloud/*` scope → 403 Forbidden. The controller gracefully skips DNS when the scope is missing |
| 29 | Cluster autoscaler annotations                        | ⚠️      | v0.4.0    | ClusterClass variables removed — CAPI MachineDeployment annotations cannot be set via ClusterClass patches. Must be set in `Cluster.spec.topology.workers.machineDeployments[].metadata.annotations`. Documented in ClusterClass comments |
| 30 | MachinePool CRD                                       | ⚠️      | v0.4.0    | CRD type defined but no controller implemented. CRD not included in kustomize default overlay. Requires `OVHMachinePoolReconciler` to be functional |
| 31 | E2E CI workflow                                       | ✅      | v0.5.0    | `.github/workflows/e2e.yml` validated end-to-end against live OVH (run 25423400091, SHA 62e39ee, 2026-05-06): pre-cleanup → cluster create → 2 Ready nodes in 463s → full delete in 136s → 0 OVH residuals. Bound to a `ovh` GitHub environment with required reviewer. See [CI integration](#ci-integration) for setup |
| 32 | `disableCloudController` ClusterClass variable        | ✅      | v0.3.1    | Writes `disable-cloud-controller: true` to RKE2 config on CP + worker. Validated live when deploying OpenStack CCM |
| 33 | Gateway expose idempotence                            | ✅      | v0.3.1    | `ExposeGateway` treats 409 Conflict as success. Eliminates ~250 spurious errors/12h in controller logs |

### Summary

| Category | Total | ✅ Pass | ⚠️ Caveat | ❌ Blocked |
|----------|-------|---------|-----------|-----------|
| Cluster lifecycle | 10 | 10 | 0 | 0 |
| HA and resilience | 3 | 2 | 0 | 0 |
| Validation | 1 | 1 | 0 | 0 |
| v0.3.0 API and integration | 5 | 5 | 0 | 0 |
| Addons (CSI, CCM) | 2 | 1 | 1 | 0 |
| BYOI | 1 | 1 | 0 | 0 |
| v0.4.0 features | 11 | 5 | 4 | 2 |
| **Total** | **33** | **25** | **5** | **2** |

**Blocked features** (OVH API limitation):
- #26 Security groups: OVH native API does not expose SGs (OpenStack Neutron only)
- #27 Instance tags: OVH native API does not expose instance metadata (OpenStack Nova only)

These require switching to the OpenStack API (gophercloud) or adding a secondary
OpenStack client alongside the OVH native client. Tracked for v0.5.0.

Bug fixes uncovered by these scenarios are documented in
[CHANGELOG.md](../CHANGELOG.md) and the [TROUBLESHOOTING.md](TROUBLESHOOTING.md)
"OVH-specific gotchas" section.

When you run a new scenario, update this table with the date it was
first validated and the release that includes the fix(es) it required.
