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

Tracked in v0.3.0 — see https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/issues
("Add LB health monitor on api-server-pool / rke2-register-pool").

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

## Where to ask for help

- GitHub issues: https://github.com/rancher-sandbox/cluster-api-provider-ovhcloud/issues
- For security issues: see [SECURITY.md](../SECURITY.md)
