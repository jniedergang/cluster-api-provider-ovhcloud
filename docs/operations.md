# Operations guide

How to install, monitor, upgrade and uninstall CAPIOVH in production.

## Installation

### Option 1 — Helm (recommended)

```bash
helm install capiovh \
  oci://ghcr.io/rancher-sandbox/charts/cluster-api-provider-ovhcloud \
  --version 0.1.0 \
  --namespace capiovh-system --create-namespace \
  --set webhooks.enabled=true \
  --set webhooks.certManager.enabled=true
```

Requires:
- CAPI core installed (`clusterctl init` or Rancher Turtles)
- cert-manager (only if `webhooks.enabled=true`)

### Option 2 — clusterctl

```bash
clusterctl init --infrastructure ovhcloud
```

This works once the provider is added to the
[`clusterctl-provider-list`](https://cluster-api.sigs.k8s.io/clusterctl/configuration.html#provider-list).
While we work on upstream submission, use the manifest directly:

```bash
kubectl apply -f https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/download/v0.1.0/infrastructure-components.yaml
```

### Option 3 — Rancher Turtles (CAPIProvider)

Prerequisites: a management cluster with [Rancher Turtles](https://turtles.docs.rancher.com/)
installed (usually via the `rancher-turtles` Helm chart under Rancher
Manager). Turtles reconciles `CAPIProvider` CRs by downloading the
release components and keeping the provider healthy.

```bash
kubectl create namespace capiovh-system
kubectl apply -f https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/download/v0.2.0/capiprovider-ovhcloud.yaml
```

The manifest template is also shipped at `templates/capiprovider-ovhcloud.yaml`:

```yaml
apiVersion: turtles-capi.cattle.io/v1alpha1
kind: CAPIProvider
metadata:
  name: ovhcloud
  namespace: capiovh-system
spec:
  name: ovhcloud
  type: infrastructure
  version: v0.2.0
  fetchConfig:
    url: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/download/v0.2.0/infrastructure-components.yaml
  configSecret:
    name: capiovh-variables
```

Set `spec.enableAutomaticUpdate: true` with a `releases/latest/download/`
URL for automatic upgrades. Verify:

```bash
kubectl -n capiovh-system get capiprovider ovhcloud
# NAME      TYPE             VERSION   PHASE     READY
# ovhcloud  infrastructure   v0.2.0    Ready     True
```

See [Rancher Turtles docs](https://turtles.docs.rancher.com/) for
`configSecret` schema and RBAC requirements.

### Addon management (CAAPF / Fleet)

CAPIOVH keeps the infrastructure controller lean. For CNI tuning, CSI
drivers, and other Helm-based addons, use the
[Cluster API Addon Provider for Fleet](https://github.com/rancher/cluster-api-addon-provider-fleet).
Full setup in [fleet-addons.md](fleet-addons.md). TL;DR:

```bash
kubectl create namespace caapf-system
kubectl apply -f https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/download/v0.2.0/capiprovider-ovhcloud.yaml
kubectl apply -f https://raw.githubusercontent.com/rancher-sandbox/cluster-api-provider-ovhcloud/v0.2.0/manifests/caapf-provider.yaml
```

## Provisioning a cluster

1. Create the OVH credentials Secret in your target namespace:

```bash
kubectl create ns demo
kubectl -n demo create secret generic ovh-credentials \
  --from-literal=endpoint=ovh-eu \
  --from-literal=applicationKey=... \
  --from-literal=applicationSecret=... \
  --from-literal=consumerKey=...
```

See [ovh-credentials-guide.md](ovh-credentials-guide.md) for how to obtain
these credentials with a properly scoped Consumer Key.

2. Apply a cluster template:

```bash
clusterctl generate cluster mycluster \
  --infrastructure ovhcloud \
  --kubernetes-version v1.31.0 \
  --control-plane-machine-count 3 \
  --worker-machine-count 2 | kubectl -n demo apply -f -
```

Or use one of the templates directly (see [../templates/](../templates/)).

3. Watch progress:

```bash
kubectl -n demo get cluster,ovhcluster,machine,ovhmachine -w
```

### Importing the workload cluster into Rancher

If you provision the cluster on a management cluster running Rancher
(via Rancher Turtles or directly), the workload cluster needs to register
with Rancher to appear in the UI. Rancher creates a
`cluster.management.cattle.io` automatically when it sees the new CAPI
`Cluster`, but the agent on the workload still needs:

1. The Rancher import manifest applied (creates `cattle-cluster-agent`)
2. (When Turtles uses `agent-tls-mode=true`, i.e. STRICT_VERIFY) the
   trusted CA bundle mounted on the agent at
   `/etc/kubernetes/ssl/certs/serverca`

#### Prerequisites (one-time Rancher setup)

The Rancher `cacerts` setting must contain the **exact TLS certificate
chain** presented by the Rancher server. Obtain it with:

```bash
curl -sk https://RANCHER_URL/v3/settings/cacerts | jq -r .value
```

If it is empty or incorrect, set it via:

```bash
CHAIN=$(echo | openssl s_client -connect RANCHER_URL:443 -showcerts 2>/dev/null \
  | awk '/BEGIN CERT/,/END CERT/')
kubectl patch setting cacerts --type=merge \
  -p "{\"value\":$(echo "$CHAIN" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read()))')}"
```

#### Per-cluster setup

**a)** Set the `rancherServerCA` topology variable on the `Cluster`.
The value **must be** the content of the Rancher `cacerts` setting
(same chain as above). When present, the ClusterClass writes
`/var/lib/rancher/rke2/server/manifests/capiovh-rancher-serverca.yaml`
on every CP node, and RKE2 auto-applies the `cattle-system/serverca`
ConfigMap on startup.

```yaml
spec:
  topology:
    variables:
      ...
      - name: rancherServerCA
        value: |
          -----BEGIN CERTIFICATE-----
          ...server cert...
          -----END CERTIFICATE-----
          -----BEGIN CERTIFICATE-----
          ...intermediate...
          -----END CERTIFICATE-----
```

Extract this value from the Rancher API:

```bash
curl -sk https://RANCHER_URL/v3/settings/cacerts | jq -r .value
```

**b)** Run the helper script to apply the import manifest **and**
patch the `cattle-cluster-agent` Deployment to mount the ConfigMap.
The script uses an `emptyDir` + `initContainer` pattern because the
agent writes to the CA path at runtime (a direct ConfigMap mount is
read-only and causes the agent to crash):

```bash
export MGMT_KUBECONFIG=/path/to/rancher-mgmt.yaml
export WORKLOAD_KUBECONFIG=/path/to/mycluster.yaml
./scripts/import-to-rancher.sh mycluster
```

The script is idempotent — safe to re-run after agent upgrades.

### Etcd backup & restore (RKE2)

RKE2 ships with built-in etcd snapshots — they're enabled by default with
the schedule `0 */5 * * *` (every 5 hours), 5-snapshot retention, stored
on each CP node at `/var/lib/rancher/rke2/server/db/snapshots/`. Tune via
the `RKE2ControlPlaneTemplate.spec.serverConfig.etcd.backupConfig` field
of your ClusterClass.

For ad-hoc snapshots and disaster recovery, use the helper script:

```bash
# List snapshots on a CP node (any of them)
./scripts/rke2-etcd-snapshot.sh list ubuntu@<cp-floating-ip>

# Trigger an on-demand snapshot
./scripts/rke2-etcd-snapshot.sh create my-snap-2026-04-14 ubuntu@<cp-floating-ip>

# Restore from a snapshot (DESTRUCTIVE — see script header)
./scripts/rke2-etcd-snapshot.sh restore my-snap-2026-04-14 ubuntu@<cp-floating-ip>
```

The restore flow is intentionally a guided manual procedure:

1. The script stops `rke2-server` on the chosen CP and runs
   `rke2 server --cluster-reset --cluster-reset-restore-path=<path>`,
   which rebuilds etcd from the snapshot and exits.
2. `rke2-server` is restarted; the CP is now a fresh single-member
   etcd cluster holding the snapshot's state.
3. **You must then delete the other CP machines via CAPI**
   (`kubectl -n fleet-default delete machine <name>`). CAPI rebuilds
   them from the ClusterClass and they join the restored cluster as
   new etcd members.

Do not try to keep the other CP machines: their old etcd data will
diverge from the snapshot.

For S3-backed snapshots (off-instance storage), see the
`backupConfig.s3` sub-field of the RKE2 server config — it accepts
endpoint, bucket, region, credential secret etc. Required for any
serious DR plan since on-disk snapshots disappear with the instance.

#### When sudo NOPASSWD is unavailable

Some OVH base images don't grant the `ubuntu` user sudo NOPASSWD even
though cloud-init declares it. As a fallback, run `rke2 etcd-snapshot`
through a privileged pod scheduled on a CP node:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: etcd-snapshot-tool
  namespace: default
spec:
  hostPID: true
  hostNetwork: true
  nodeSelector:
    node-role.kubernetes.io/control-plane: "true"
  tolerations: [{operator: Exists}]
  containers:
  - name: tool
    image: docker.io/library/alpine:3.20
    command: ["sleep","3600"]
    securityContext: {privileged: true}
    volumeMounts: [{name: host, mountPath: /host}]
  volumes:
  - name: host
    hostPath: {path: /}
```

Then:

```bash
kubectl exec -n default etcd-snapshot-tool -- \
  chroot /host /usr/local/bin/rke2 etcd-snapshot list
kubectl exec -n default etcd-snapshot-tool -- \
  chroot /host /usr/local/bin/rke2 etcd-snapshot save --name=my-snap
```

## Monitoring

### Prometheus metrics

The controller exposes metrics on `:8080/metrics` (configurable via
`--metrics-bind-address`):

| Metric | Type | Description |
|--------|------|-------------|
| `capiovh_machine_create_total` | Counter | Total instance creation attempts |
| `capiovh_machine_create_errors_total` | Counter | Instance creation errors |
| `capiovh_machine_creation_duration_seconds` | Histogram | Time from POST to ACTIVE |
| `capiovh_machine_delete_total` | Counter | Total instance deletion attempts |
| `capiovh_machine_status` | Gauge | 1 if machine is Ready, 0 otherwise |
| `capiovh_cluster_ready` | Gauge | 1 if cluster is Ready, 0 otherwise |
| `capiovh_machine_reconcile_duration_seconds` | Histogram | Reconcile duration (`operation` label: `normal` or `delete`) |
| `capiovh_cluster_reconcile_duration_seconds` | Histogram | Cluster reconcile duration |
| `capiovh_node_init_duration_seconds` | Histogram | Workload node init duration (providerID + taint) |
| `capiovh_etcd_member_removal_duration_seconds` | Histogram | etcd member removal duration on CP deletion |
| `capiovh_bootstrap_wait_duration_seconds` | Histogram | OVH instance BUILD → ACTIVE duration |
| `capiovh_lb_poll_duration_seconds` | Histogram | LB find-by-name polling duration after async POST |
| `capiovh_ovh_api_requests_total` | CounterVec | OVH API calls by `endpoint` and `outcome` (`ok`/`error`/`retry`) |
| `capiovh_ovh_api_request_duration_seconds` | HistogramVec | OVH API call latency by `endpoint` |

A `ServiceMonitor` for Prometheus Operator is shipped as a conditional
Helm template. Enable with:

```bash
helm upgrade capiovh oci://ghcr.io/rancher-sandbox/charts/cluster-api-provider-ovhcloud \
  --set metrics.serviceMonitor.enabled=true
```

The raw kustomize overlay is at `config/prometheus/` (uncomment the
reference in `config/default/kustomization.yaml`).

### NetworkPolicy for metrics scraping

A `NetworkPolicy` restricting ingress on the metrics port to namespaces
labelled `metrics: enabled` is available via `networkPolicy.enabled=true`
(Helm) or the `config/network-policy/` overlay (kustomize, wired into
`config/default`). Label your Prometheus namespace accordingly:

```bash
kubectl label namespace monitoring metrics=enabled
```

### Logs

```bash
# Live tail
kubectl -n capiovh-system logs -f deploy/capiovh-controller-manager

# Only errors
kubectl -n capiovh-system logs deploy/capiovh-controller-manager | grep -i error

# Specific cluster
kubectl -n capiovh-system logs deploy/capiovh-controller-manager | grep mycluster
```

The controller uses zap with `Development=true` by default; structured
JSON output can be enabled by setting `--zap-encoder=json`.

## Upgrade

### Helm

```bash
helm upgrade capiovh \
  oci://ghcr.io/rancher-sandbox/charts/cluster-api-provider-ovhcloud \
  --version 0.2.0 \
  --namespace capiovh-system \
  --reuse-values
```

Always check the [release notes](https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases)
for breaking changes before upgrading.

### Manifest-based

```bash
kubectl apply -f https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/releases/download/v0.2.0/infrastructure-components.yaml
```

Existing CRs are preserved; the controller re-reconciles them with the
new version. CRD changes (rare for v0.x) are forward-compatible thanks
to the additive nature of CAPI v1beta1.

## Uninstall

```bash
# 1. Delete all clusters managed by the provider first
kubectl get cluster -A -l cluster.x-k8s.io/provider=infrastructure-ovhcloud
# (delete each one)

# 2. Wait for cleanup to complete (no OVHMachine / OVHCluster left)
kubectl get ovhcluster,ovhmachine -A

# 3. Uninstall the provider
helm uninstall capiovh -n capiovh-system

# 4. Remove CRDs (Helm convention is to NOT remove them automatically)
kubectl delete crd ovhclusters.infrastructure.cluster.x-k8s.io
kubectl delete crd ovhclustertemplates.infrastructure.cluster.x-k8s.io
kubectl delete crd ovhmachines.infrastructure.cluster.x-k8s.io
kubectl delete crd ovhmachinetemplates.infrastructure.cluster.x-k8s.io
```

## Backup / disaster recovery

The provider is stateless; all state lives in:

- The management cluster's etcd (CRD instances)
- The OVH project (instances, network, LB)

For DR:
- Back up the management cluster (Velero or similar)
- Re-deploy CAPIOVH and re-apply CRs after restore — the controller
  will re-discover existing OVH resources via list-by-name
  (idempotent reconciliation)

## Tuning

| Knob | Default | Effect |
|------|---------|--------|
| `replicas` | 1 | Set to 2+ for HA. `leaderElect=true` ensures only one is active. |
| `resources.limits.memory` | 256Mi | Increase if you have many clusters or large reconcile cycles. |
| `--reconcile-interval` (default ~30s requeue) | hardcoded | Not currently configurable. |

## Observability via Grafana

A pre-built Grafana dashboard (`config/grafana/capiovh-dashboard.json`,
UID `capiovh-overview`) ships 21 panels grouped into 5 rows:

- **Machine Lifecycle**: instance create/delete totals, errors, creation duration (p50/p90/p99), current machine status
- **Reconciliation Performance**: machine + cluster reconcile durations (p50/p90)
- **OVH API & Cluster**: OVH API success + error counters, LB poll count, cluster Ready gauge
- **etcd & Node Init**: etcd member removal duration, bootstrap wait count, node init duration (p50/p90)
- **Reconciliation Rate**: rate(create) / rate(delete) / rate(API calls)

Import it via `grafana.com` JSON import, or programmatically:

```bash
curl -X POST -H "Authorization: Bearer $GRAFANA_TOKEN" \
  -H "Content-Type: application/json" \
  -d @config/grafana/capiovh-dashboard.json \
  http://grafana:3000/api/dashboards/db
```

A Prometheus datasource variable (`datasource`) is templated — select
your Prometheus instance when first opening the dashboard.

## OpenStack credentials for CSI and CCM

OVH Public Cloud is OpenStack-based. The upstream **Cinder CSI** and
**OpenStack Cloud Controller Manager** (CCM) work with OVH, but
require OpenStack credentials distinct from the OVH API credentials
used by CAPIOVH itself.

### Obtaining OpenStack credentials

1. Log in to the [OVH Manager](https://www.ovh.com/manager/)
2. Navigate to **Public Cloud > Project Settings > Users & Roles**
3. Create a user with the `objectstore_operator` and
   `compute_operator` roles
4. Download the **OpenStack RC file v3** (Identity API v3)
5. Or generate application credentials:
   ```bash
   openstack application credential create capiovh-csi \
     --unrestricted
   ```

### Creating the cloud-config Secret

On each **workload** cluster, create the Secret before deploying the
CSI or CCM addon:

```bash
kubectl create secret generic cloud-config \
  --namespace=kube-system \
  --from-literal=cloud.conf="$(cat <<'EOF'
[Global]
auth-url = https://auth.cloud.ovh.net/v3
application-credential-id = <ID>
application-credential-secret = <SECRET>
region = <REGION>

[BlockStorage]
bs-version = v3

[LoadBalancer]
use-octavia = true
subnet-id = <SUBNET_ID>
floating-network-id = <EXT_NET_ID>
EOF
)"
```

The `[LoadBalancer]` section is only needed when deploying the CCM
for `Service type=LoadBalancer` support. For CSI-only deployments,
omit it.

### Addon manifests

Example Helm charts for both addons are provided in
`templates/addons/`:

| Addon | File | Purpose |
|-------|------|---------|
| Cinder CSI | `cinder-csi-helmchartconfig.yaml` | PersistentVolume provisioning via OVH block storage |
| OpenStack CCM | `openstack-ccm-helmchartconfig.yaml` | Service type=LoadBalancer via Octavia, node lifecycle |

Both can be deployed via Fleet (see `templates/addons/README.md`) or
applied directly as RKE2 HelmChart CRs on the workload cluster.
