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

E2E is run manually before each release. Automating it in CI would require
a dedicated OVH project budget; not currently planned.

## Production readiness validation matrix

Manual scenarios run on a real OVH project against a live cluster, to
exercise behaviors that automated tests do not cover (rollouts, network,
HA, multi-tenancy). Each row is a one-shot scenario; results are recorded
in the release CHANGELOG entry rather than continuously re-run.

Status legend: ✅ passed live | ⚠️ passed with caveat | ❌ blocked | ⏳ planned

| #  | Scenario                                              | Status | First validated | Notes |
|----|-------------------------------------------------------|--------|-----------------|-------|
| 1  | Cluster create via Rancher UI (1 CP + 1 worker)       | ✅      | v0.2.0          | ~7 min on v1.32.4+rke2r1 |
| 2  | Cluster create via kubectl (1 CP + 1 worker)          | ✅      | v0.2.0          | ~10 min |
| 3  | Cluster delete + 0 OVH residual                       | ✅      | v0.2.2          | FIP cleanup async-DELETE quirk handled correctly via convergence fix (treats `detached + down` as already-deleted). Re-validated v0.2.2 with parallel teardown of 2 clusters: 7 ovhmachines → 0 in 90 s; 2 ovhclusters → 0 in 195 s; only async-reap FIPs remain in OVH (no leak) |
| 4  | Scale CP 1→3 + worker 1→2                             | ✅      | v0.2.2          | Re-tested live: workers 1→2 in ~2 min on warm cluster |
| 5  | kubectl from external host via LB FIP                 | ✅      | v0.2.2          | Cert SAN includes FIP IP. Re-tested live |
| 5b | kubectl via Rancher proxy                             | ✅      | v0.2.2          | All 4 nodes visible as `nodes.management.cattle.io` |
| 6  | MachineDeployment self-heal (delete worker)           | ✅      | v0.2.2          | CAPI MD recreates Machine after explicit delete. Re-tested v0.2.2: drain blocks ~3 min on tigera-operator (single-replica, no PDB) until a 2nd worker becomes available. MachineHealthCheck CRs now ship in all templates including ClusterClass (v0.3.0) |
| 7  | k8s in-place upgrade (v1.33.10 → v1.34.6)             | ✅      | v0.2.2          | **15 m 32 s** total for 3 CPs + 2 workers. **100 % Rancher connectivity throughout** (Conn=True/Ready=True every poll). Each CP swap ~5 min: provision + etcd-join + drain + etcd-remove + delete |
| 8  | Multi-cluster in same OVH project                     | ✅      | v0.2.2          | Requires distinct `vlanID` topology variable per cluster. Re-tested v0.2.2: cluster-2 (vlanID=200) created in 6 m 30 s to controlPlaneReady, +170 s to Rancher Active. `rancherServerCA` topology variable auto-creates `cattle-system/serverca` ConfigMap on workload — agent connects without manual intervention |
| 9  | Webhook + CRD validation rejects bad input            | ✅      | v0.2.2          | 16/16 cases via `test/e2e/run-validation-tests.sh`. Found CRD apply quirk: `kubectl apply` may not always propagate new properties — use `kubectl replace` to refresh schema |
| 10 | HA control-plane survives 1 CP failure                | ✅      | v0.2.2          | 3/3 CP recovers in 4m21s with **100 % API availability** measured via LB FIP (260 probes, 0 timeout) thanks to the Octavia health monitor (TCP, 5 s/3 s/2 retries, ~10 s detection). Without HM (v0.2.1) the same scenario was 14m12s and 52 % availability |
| 11 | Etcd snapshot + restore                               | ⚠️      | v0.2.2          | List/create validated live (`manual-test-...-1776203719`, 10 MB on disk). Restore is documented and scripted (`scripts/rke2-etcd-snapshot.sh restore`) but intentionally not executed live to avoid destroying the running validation cluster. Note: OVH ubuntu image may not grant sudo NOPASSWD; fallback via privileged hostPID pod is documented in operations.md |
| 12 | PVC via OVH block storage CSI                         | ⏳      | —               | Cinder CSI addon manifest shipped in v0.3.0 (`templates/addons/cinder-csi-helmchartconfig.yaml`). Requires OpenStack credentials (see `docs/operations.md`). Pending live validation |
| 13 | Service type=LoadBalancer via cloud-controller        | ⏳      | —               | OpenStack CCM addon manifest shipped in v0.3.0 (`templates/addons/openstack-ccm-helmchartconfig.yaml`). Requires OpenStack credentials (see `docs/operations.md`). Pending live validation |
| 14 | Multi-cluster simultaneous delete + cleanup           | ✅      | v0.2.2          | Both clusters cascade cleanly. FIP convergence fix validated live: controller correctly distinguishes `attached/active → retry` from `detached/down → treat as deleted`. Lingering OVH-side `down` FIPs are reaped async by OVH within minutes; no manual intervention. Pre-existing OVH 409 "Port has device owner router_centralized_snat" race during network delete is auto-recovered on next reconcile |
| 15 | Scheduler stress (50-pod deployment)                  | ✅      | v0.2.2          | 50/50 pause pods Running in 12s, distributed across 3 CPs + 2 workers (CPs accept tolerations:Exists). Validates pod CIDR sizing (10.244.0.0/16) and scheduler throughput |
| 16 | BYOI image (custom snapshot)                          | ⏳      | —               | Code path exists in `pkg/ovh.GetImageByName`, never live-tested |
| 17 | 24 h soak (no leak, no OOMKilled, certs stable)       | ⏳      | —               | Long-running observability via Grafana |

Bug fixes uncovered by these scenarios are documented in
[CHANGELOG.md](../CHANGELOG.md) and the [TROUBLESHOOTING.md](TROUBLESHOOTING.md)
"OVH-specific gotchas" section.

When you run a new scenario, update this table with the date it was
first validated and the release that includes the fix(es) it required.
