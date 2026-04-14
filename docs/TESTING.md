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
| 3  | Cluster delete + 0 OVH residual                       | ✅      | v0.2.0          | FIP cleanup is async, controller retries |
| 4  | Scale CP 1→3 + worker 1→2                             | ✅      | v0.2.0          | ~4 min on warm cluster (image cache) |
| 5  | kubectl from external host via LB FIP                 | ✅      | v0.2.0          | Cert SAN includes FIP IP |
| 5b | kubectl via Rancher proxy                             | ⚠️      | v0.2.0          | Works at first import; tunnel can flake post-rollout (Rancher-side) |
| 6  | MachineHealthCheck remediation (delete worker)        | ✅      | v0.2.0          | Net 30 s after drain unblocks; needs PDB on single-replica addons |
| 7  | k8s in-place upgrade (v1.32.4 → v1.33.10)             | ✅      | v0.2.0          | ~9 min total; ~30 s API window on single-CP swap |
| 8  | Multi-cluster in same OVH project                     | ✅      | v0.2.1          | Requires distinct `vlanID` topology variable per cluster |
| 9  | Webhook + CRD validation rejects bad input            | ✅      | v0.2.1          | 16/16 cases via `test/e2e/run-validation-tests.sh`. Found CRD apply quirk: `kubectl apply` may not always propagate new properties — use `kubectl replace` to refresh schema |
| 10 | HA control-plane survives 1 CP failure                | ⚠️      | v0.2.1          | 3/3 CP recovers in 14m12s. Workload kubectl preserved. BUT: API availability via LB FIP drops to ~52% during the window because Octavia pool members are `noMonitor` — LB round-robins to dead/booting CPs. Real HA needs LB health monitor (tracked separately) |
| 11 | Etcd snapshot + restore                               | ⏳      | —               | RKE2 takes scheduled snapshots; validate disaster recovery |
| 12 | PVC via OVH block storage CSI                         | ⏳      | —               | No CSI installed today; gap report for v0.3.0 |
| 13 | Service type=LoadBalancer via cloud-controller        | ⏳      | —               | No CCM-OVH today; gap report for v0.3.0 |
| 14 | Multi-cluster simultaneous delete + cleanup           | ⚠️      | v0.2.1          | Both clusters fully cascade (CRs gone, instances/LBs cleaned) but FIP retry loop still hits the OVH async-DELETE quirk: status reports `down` while the resource lingers. Manual `ovh DELETE /floatingip/{id}` is needed to expedite. Same root cause as the FIP cleanup limitation tracked under v0.3.0 |
| 15 | Scheduler stress (50-pod deployment)                  | ⏳      | —               | Pod CIDR / etcd write throughput sanity |
| 16 | BYOI image (custom snapshot)                          | ⏳      | —               | Code path exists in `pkg/ovh.GetImageByName`, never live-tested |
| 17 | 24 h soak (no leak, no OOMKilled, certs stable)       | ⏳      | —               | Long-running observability via Grafana |

Bug fixes uncovered by these scenarios are documented in
[CHANGELOG.md](../CHANGELOG.md) and the [TROUBLESHOOTING.md](TROUBLESHOOTING.md)
"OVH-specific gotchas" section.

When you run a new scenario, update this table with the date it was
first validated and the release that includes the fix(es) it required.
