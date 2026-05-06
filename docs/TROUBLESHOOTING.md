# Troubleshooting

Common issues and how to diagnose / fix them.

## OVHCluster stuck in `status.ready=false`

Check the conditions:

```bash
kubectl -n <ns> get ovhcluster <name> -o jsonpath='{.status.conditions}' | jq
```

Then look at the controller logs:

```bash
kubectl -n capiovh-system logs deploy/capiovh-controller-manager --tail=50
```

### `OVHConnectionReady=False`

Cause: the OVH API rejected the request.

Most common reasons:

- **`This call has not been granted` (403)**: the Consumer Key access
  rules don't include the path being called. Check the rules with
  `GET /auth/currentCredential`. The Consumer Key MUST cover at minimum
  `GET/POST/PUT/DELETE /cloud/project/{serviceName}/*`. See
  [ovh-credentials-guide.md](ovh-credentials-guide.md).
- **Trailing space in rules**: rules like `/cloud/project/SN/* ` (with a
  trailing space) match nothing. Recreate the Consumer Key with clean
  paths.
- **`This application key is invalid` (403)**: the AK is for a different
  endpoint. Check `OVH_ENDPOINT` matches where the AK was created
  (`ovh-eu`, `ovh-ca`, `ovh-us`).

### `NetworkReady=False`, message "region activation in progress"

Cause: just after `CreatePrivateNetwork`, the network needs ~30-60 seconds
to be activated in the target region before subnet creation succeeds. The
controller will retry automatically; just wait.

### `LoadBalancerReady=False`, status stuck on `creating`

Cause: Octavia LB creation can take 1-3 minutes. If it's been longer,
check directly in OVH:

```bash
curl ... /cloud/project/$SN/region/$REGION/loadbalancing/loadbalancer
```

If the LB is in `error` state, OVH support may need to investigate. The
controller will not auto-recover; manually delete the LB and the
`OVHCluster.status.loadBalancerID` will be cleared on next reconcile.

## OVHMachine stuck in `BUILD`

`status.instanceID` is set, but the instance never reaches ACTIVE.

Check:

```bash
kubectl -n <ns> get ovhmachine <name> -o jsonpath='{.status.instanceID}'
# Then in OVH:
curl ... /cloud/project/$SN/instance/<instanceID>
```

If OVH reports `ERROR`, the controller marks the OVHMachine as failed.
Check the OVH Manager UI for the underlying cause (no quota, image
unavailable in region, ...). Delete the OVHMachine and re-create.

## "no endpoints available for service ... webhook-service"

Cause: webhook is enabled but the controller pod is not yet Ready, or
the cert-manager Certificate is not yet `True`.

Check:

```bash
kubectl -n capiovh-system get pods,certificate
```

Wait until both are Ready. The `cert-manager.io/inject-ca-from`
annotation injects the CA only after the Certificate is signed.

## Image not found: `image "openSUSE-Leap-15.6" not found`

Cause: the image is not in the OVH catalog and not uploaded as a custom
image (BYOI) in your project.

Fix: upload the image via OpenStack Glance (see
[byoi-guide.md](byoi-guide.md)). The provider searches both `/image`
(public catalog) and `/snapshot` (BYOI) automatically; the upload must be
visible under one of these.

## Orphan LBs in OVH after Cluster deletion

Should not happen anymore as of v0.1.0 (cleanup-orphan logic in
ReconcileDelete), but if you have leftovers from earlier versions:

```bash
# List all LBs with the capi prefix
curl ... /cloud/project/$SN/region/$REGION/loadbalancing/loadbalancer \
  | jq '.[] | select(.name | startswith("capi-"))'

# Delete one
curl -X DELETE ... /cloud/project/$SN/region/$REGION/loadbalancing/loadbalancer/<id>
```

LBs in `pending_create` or `pending_update` state cannot be deleted; wait
for them to reach `active` first.

## `cannot find any versions matching contract cluster.x-k8s.io/v1beta1`

Cause: the CAPI Cluster controller cannot resolve the InfraCluster
reference because the CRD is missing the contract version label.

Fix: ensure the `cluster.x-k8s.io/v1beta1: v1alpha1` label is set on
all 4 CRDs:

```bash
kubectl get crd ovhclusters.infrastructure.cluster.x-k8s.io \
  -o jsonpath='{.metadata.labels}' | jq
```

If missing, re-apply with the kustomize / Helm bundle which sets it
automatically.

## Cluster controller not propagating OwnerRef

Restart the CAPI core controller after installing CAPIOVH:

```bash
kubectl -n cattle-capi-system rollout restart deploy/capi-controller-manager
# or, if installed via clusterctl:
kubectl -n capi-system rollout restart deploy/capi-controller-manager
```

The CAPI controller caches the discovered CRDs at startup; after
installing a new infrastructure provider it needs to re-discover.

## OVH-specific gotchas

These behaviors are specific to OVH Public Cloud (vRack networking) and
were uncovered during live E2E validation. Most are transparently handled
by CAPIOVH ≥ v0.1.3, but worth knowing if you debug a stuck cluster.

### Pod CIDR must NOT overlap with vRack subnet

The default RKE2 pod CIDR is `10.42.0.0/16`. If your CAPIOVH cluster also
uses `10.42.0.0/24` for the vRack subnet (the default `subnetCIDR`
variable), Calico's `natOutgoing` rule sees the node IPs as inside its
IP pool and **skips SNAT**. Pods then send to the kubernetes ClusterIP
service with their pod source IP, OVH neutron port-security drops the
egress, and pods cannot reach the API.

Symptoms: `cattle-cluster-agent` / `coredns` / any pod-to-API timeouts;
`MachineDeployment` shows nodes Ready but Rancher import never completes.

**Fix in v0.1.3+**: the bundled ClusterClass writes
`/etc/rancher/rke2/config.yaml.d/10-cidrs.yaml` with `cluster-cidr: 10.244.0.0/16`
and `service-cidr: 10.96.0.0/16` so they never overlap with the default
`10.42.0.0/24` vRack subnet. If you change `subnetCIDR`, ensure your pod
CIDR (set in your fork of the ClusterClass) also stays disjoint.

### kubelet provider-id must be set explicitly

RKE2 registers each Node with `providerID=rke2://<hostname>` by default.
CAPI needs `providerID=ovhcloud://<region>/<instance-uuid>` to link
`Machine` ↔ `Node`. Without it, `MachineDeployment` stays in `ScalingUp`
forever and `MachineHealthCheck` cannot remediate.

**Fix in v0.1.3+**: a `preRKE2Commands` snippet fetches the instance UUID
from OVH OpenStack metadata (`http://169.254.169.254/openstack/latest/meta_data.json`)
and writes `/etc/rancher/rke2/config.yaml.d/90-provider-id.yaml` with
`kubelet-arg: provider-id=...` before RKE2 starts. The cluster region is
written to `/etc/capiovh/region` via a ClusterClass JSONPatch.

### Floating IP cleanup is async (and often slow)

`DELETE /cloud/project/{id}/region/{region}/floatingip/{fipID}` returns
`200 OK` immediately but the actual removal may take several minutes,
and a FIP attached to a load balancer in `PENDING_DELETE` state is
"detached" rather than removed.

**Fix in v0.1.3+**: `OVHCluster` `ReconcileDelete` captures all FIPs
associated with the cluster's LB **before** deleting the LB, then loops
on `GetFloatingIP` after each `DeleteFloatingIP` call and requeues if
the resource is still present. Manual cleanup is rarely needed but in
extreme cases:

```bash
ovh_get "/cloud/project/$SVC/region/$REG/floatingip" \
  | jq -r '.[] | select(.associatedEntity == null) | .id' \
  | xargs -I{} ovh_delete "/cloud/project/$SVC/region/$REG/floatingip/{}"
```

### DNS not provided by vRack DHCP

vRack DHCP only hands out a default route; no DNS server. Cloud-init's
default `systemd-resolved` will not be able to resolve `github.com` (used
by the RKE2 install script) until reconfigured.

**Fix in v0.1.3+**: `preRKE2Commands` overwrites `/etc/resolv.conf` with
`1.1.1.1 8.8.8.8 9.9.9.9` BEFORE running `curl get.rke2.io | sh`, then
synchronously polls until `getent hosts github.com` resolves.

### Octavia pool members have no health monitor (HA caveat)

The CAPIOVH controller currently creates Octavia LB pools without a
health monitor (`operatingStatus: noMonitor`). The LB round-robins
traffic to ALL pool members regardless of their actual health.

Practical impact:
* During CP rolling updates / failover, ~50 % of API requests via the
  LB FIP can hit a dead or booting backend → 5 s timeout per request.
* `cattle-cluster-agent` tunnels via the FIP can flap; the cluster may
  show as `Connected=False` in Rancher UI for a while after a CP swap.
* In-cluster traffic via the kubernetes ClusterIP is unaffected (it
  uses kube-proxy + endpointslices, which DO track health).

Workarounds until the controller adds a health monitor:
* Run with 3+ CP replicas so quorum survives one bad backend.
* For Rancher integration: re-restart `cattle-cluster-agent` after a
  CP rolling update completes.
* For external clients: implement client-side retry on 5xx / network
  errors.

**Resolved in v0.2.2**: LB pool health monitors are now enabled by
default (TCP, delay 5s, timeout 3s, max 2 retries).

### Rancher integration: cattle-cluster-agent needs serverca

When Rancher uses a custom or Let's-Encrypt-issued cert with
`STRICT_VERIFY=true`, the import-manifest's `cattle-cluster-agent`
Deployment expects to find the trusted CA bundle at
`/etc/kubernetes/ssl/certs/serverca`. Without it, the agent crashloops
with `unable to read CA file from /etc/kubernetes/ssl/certs/serverca`
and the cluster stays Pending in Rancher UI.

**Fix in v0.1.3+**:

1. Set `rancherServerCA` topology variable on the `Cluster` to the PEM CA
   bundle. CAPIOVH will create the `cattle-system/serverca` ConfigMap
   automatically via an RKE2 auto-apply manifest.
2. Run `scripts/import-to-rancher.sh <cluster-name>` to apply the
   import URL **and** patch the agent Deployment to mount the ConfigMap.
   The script is idempotent.

### `SSH key "..." not found` — even though `openstack keypair list` shows it

OVH has **two parallel SSH key inventories**:

- The **OpenStack Nova** keypair store, populated by
  `openstack keypair create`. Visible only via the OpenStack API.
- The **OVH native** SSH key store at `/cloud/project/{sn}/sshkey`,
  populated by the OVH manager UI or `POST /cloud/project/{sn}/sshkey`.

CAPIOVH uses the OVH native API to resolve the SSH key by name. A keypair
created via `openstack keypair create` will not appear in the native API
and the controller will fail with `resolving SSH key "<name>": SSH key
"<name>" not found` — even though `openstack keypair list` happily lists it.

**Fix**: register the public key via the OVH native API:

```bash
curl -X POST https://eu.api.ovh.com/1.0/cloud/project/$SN/sshkey \
  -H "X-Ovh-Application: $AK" -H "X-Ovh-Consumer: $CK" \
  -H "X-Ovh-Timestamp: $TS"  -H "X-Ovh-Signature: $SIG" \
  -H "Content-Type: application/json" \
  -d '{"name":"capiovh-e2e","publicKey":"ssh-ed25519 AAAA... me","region":"EU-WEST-PAR"}'
```

See [ovh-credentials-guide.md](ovh-credentials-guide.md#ssh-key-pitfall).

### SSH key found in one region but not the other

OVH SSH keys are **region-scoped**. The `regions` field on the SSH key
record lists every region the key is registered in. A key created in
`EU-WEST-PAR` is invisible from `GRA9` reconciles and vice versa. If the
controller flips between regions (e.g. via `inputs.region` in the e2e
workflow), make sure the key exists in every target region — or
re-register it via `POST /cloud/project/{sn}/sshkey` with the new region.

### `A private network already exists for this vlan ID`

OVH considers `vlanId` to be **project-scoped, not region-scoped**.
Creating a private network with `vlanId: 0` in `GRA9`, then attempting
the same `vlanId: 0` in `EU-WEST-PAR` returns:

```
400 Client::BadRequest: A private network already exists for this vlan ID (network id pn-NNNNNN_0)
```

**Fix**: use distinct `vlanId` per cluster (the
`templates/clusterclass/rke2/clusterclass-ovhcloud-rke2.yaml` already
exposes a `vlanID` topology variable for this), or delete the existing
network first. The DELETE path expects the **full vlanId-suffixed ID**:
`DELETE /cloud/project/{sn}/network/private/pn-NNNNNN_0`, not
`pn-NNNNNN`.

### Network DELETE returns `Port X has device owner network:router_interface_distributed`

Symptom: trying to delete an OVH private network returns 409 with the
error above. Cause: a CAPIOVH-created internal router (named
`capi-<cluster>-gw`) still has an interface on the subnet.

**Cleanup ordering** (also implemented in
[`test/e2e/pre-cleanup.sh`](../test/e2e/pre-cleanup.sh)):

1. `DELETE` the load balancer (async, ~30–60 s).
2. Wait for the LB list to become empty.
3. `DELETE` every floating IP for the project/region.
4. Find the router with `openstack router list` (filter by
   `capi-<cluster>-gw`), then `openstack router remove subnet ROUTER
   SUBNET` followed by `openstack router delete ROUTER`. The OVH
   native API has no router endpoint — Neutron is the only path.
5. `DELETE /cloud/project/{sn}/network/private/{netId}_{vlanId}`.

### Floating IPs accumulate after multiple test runs

Each LB the controller creates allocates a FIP, which in turn binds an
internal Neutron router. Default project quota is `maxGateways: 2`. After
~2 leaked FIPs (or even sometimes after a clean delete, due to OVH async
reaping latency), new LB FIP allocations 409 with:

```
Client::MaxQuotaReached::Router: "conflict: max quota of 2 reached for: router"
```

`openstack router list` may even show **zero** routers at this point —
OVH's internal accounting and the user-visible Neutron list are not the
same set of objects.

**Fix**: run the pre-cleanup script. FIPs in `status: down` with
`associatedEntity: null` are CAPIOVH-deleted and pending OVH async reap;
they are not real leaks but they do hold the quota slot until OVH garbage
collects them (a few minutes). Pre-cleanup deletes them explicitly so the
next run starts on a clean slate.

### Load balancer wedged in `PENDING_CREATE`

OVH occasionally leaves a load balancer in `provisioningStatus:
PENDING_CREATE` indefinitely (observed: 30+ minutes after the create
POST, no further updates). In this state OVH refuses any `DELETE`:

```
409 Client::Conflict: "Invalid state PENDING_CREATE of loadbalancer resource <id>"
```

This is an OVH-side issue, not a CAPIOVH bug. The wedged LB occupies one
slot in the per-project LB quota (default `maxLoadbalancers: 5`); fresh
test runs use timestamped names so a stuck LB never blocks a new one.

The pre-cleanup script skips LBs in `PENDING_CREATE` / `PENDING_DELETE`
with a warning. If they become a quota problem, open a ticket with OVH
support to force-delete them.

### Cluster reaches `Provisioned` but `RKE2ControlPlane.status.initialized` stays empty

The OVHCluster reaches Ready, the OVH instance reaches ACTIVE, but the
control plane never initializes. The CAPIOVH controller logs repeatedly
emit `failed to get workload node for init: dial tcp <fipAddr>:6443:
i/o timeout` (or `EOF`).

The infrastructure is fine: kube-apiserver hasn't started inside the
instance. Causes seen in practice:

- LB provisioning took 14+ minutes and the test timed out before RKE2
  could install. **Fix**: `TIMEOUT_CLUSTER_READY=1800` (already the
  default) gives RKE2 ~20 min after the LB becomes ACTIVE.
- cloud-init failed to fetch the RKE2 binary because DNS in the VM
  pointed at the OVH default resolver (no public DNS). See "DNS
  resolution failure inside RKE2 nodes (vRack DHCP)" above. Fixed in
  v0.1.3+.
- Wrong SSH key registered → instance booted but cloud-init had the
  wrong fingerprint. See SSH key sections above.

To diagnose live: trigger the e2e workflow which automatically SSH-
collects `cloud-init-output.log` and the `rke2-server` journal from the
failed instance — see [TESTING.md](TESTING.md#ci-integration).

## Where to ask for help

- GitHub issues: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/issues
- For security issues: see [SECURITY.md](../SECURITY.md)
