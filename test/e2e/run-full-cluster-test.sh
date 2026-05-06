#!/usr/bin/env bash
# Autonomous end-to-end test: create a full CAPIOVH cluster (CP+worker)
# via kubectl apply, wait for all nodes Ready, then delete and verify
# cleanup. Faster than UI-based testing and idempotent across runs.
#
# Required env:
#   KUBECONFIG                Path to management cluster kubeconfig (must have
#                             CAPIOVH CAPIProvider + ClusterClass installed)
#   OVH_ENDPOINT              e.g. ovh-ca
#   OVH_APP_KEY               OVH Application Key
#   OVH_APP_SECRET            OVH Application Secret
#   OVH_CONSUMER_KEY          OVH Consumer Key
#   OVH_SERVICE_NAME          OVH project ID
#   OVH_REGION                e.g. EU-WEST-PAR
#   OVH_FLOATING_NETWORK_ID   External network UUID for the LB floating IP
#   OVH_SSH_KEY               Registered SSH key name on OVH
#
# Usage:
#   export all env vars, then
#   ./test/e2e/run-full-cluster-test.sh [cluster_name]
#
# Exit codes: 0 = all Ready, cluster deleted, OVH empty. 1 = any failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=utils.sh
source "${SCRIPT_DIR}/utils.sh"

CLUSTER_NAME="${1:-capitest-$(date +%s)}"
NAMESPACE="fleet-default"
K8S_VERSION="${K8S_VERSION:-v1.31.4+rke2r1}"

TIMEOUT_CLUSTER_READY=900 # 15 min
TIMEOUT_NODES_READY=600   # 10 min after CP ready
TIMEOUT_DELETE=600        # 10 min for full delete
POLL_INTERVAL=15

log_info "=== Test: create full cluster $CLUSTER_NAME, wait Ready, delete, verify ==="

# ---- Step 1: ensure credentials Secret in fleet-default ----
log_info "Creating ovh-credentials Secret in $NAMESPACE"
kubectl -n "$NAMESPACE" create secret generic ovh-credentials \
  --from-literal=endpoint="$OVH_ENDPOINT" \
  --from-literal=applicationKey="$OVH_APP_KEY" \
  --from-literal=applicationSecret="$OVH_APP_SECRET" \
  --from-literal=consumerKey="$OVH_CONSUMER_KEY" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

# ---- Step 2: apply Cluster manifest ----
log_info "Applying Cluster $CLUSTER_NAME"
cat <<EOF | kubectl apply -f -
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: ${CLUSTER_NAME}
  namespace: ${NAMESPACE}
spec:
  topology:
    class: ovhcloud-rke2
    classNamespace: ${NAMESPACE}
    version: ${K8S_VERSION}
    controlPlane:
      replicas: 1
    workers:
      machineDeployments:
        - class: default-worker
          name: worker
          replicas: 1
    variables:
      - name: serviceName
        value: "${OVH_SERVICE_NAME}"
      - name: region
        value: "${OVH_REGION}"
      - name: identitySecretName
        value: "ovh-credentials"
      - name: subnetCIDR
        value: "10.42.0.0/24"
      - name: lbFlavor
        value: "small"
      - name: floatingNetworkID
        value: "${OVH_FLOATING_NETWORK_ID}"
      - name: cpFlavor
        value: "b3-8"
      - name: workerFlavor
        value: "b3-8"
      - name: image
        value: "Ubuntu 22.04"
      - name: sshKeyName
        value: "${OVH_SSH_KEY}"
EOF

# ---- Step 3: wait for OVHCluster Ready, CP init=true, 2 nodes Ready ----
start=$(date +%s)
last_state=""
last_log=$start

# Disable -e/-pipefail inside the polling loop. Transient kubectl/apiserver
# blips (RKE2 supervisor restart, etcd leader rotation, secret not yet
# materialized, kube-apiserver TLS handshake EOF on a freshly-booting CP)
# must not abort the whole test — keep polling until success or timeout.
set +eo pipefail

while true; do
  elapsed=$(( $(date +%s) - start ))
  if [ "$elapsed" -gt "$TIMEOUT_CLUSTER_READY" ]; then
    set -eo pipefail
    fail_test "timeout after ${elapsed}s waiting for cluster Ready"
    break
  fi

  phase=$(kubectl -n "$NAMESPACE" get cluster "$CLUSTER_NAME" -o jsonpath='{.status.phase}' 2>/dev/null)
  ovhc_ready=$(kubectl -n "$NAMESPACE" get ovhcluster -l cluster.x-k8s.io/cluster-name="$CLUSTER_NAME" -o jsonpath='{.items[0].status.ready}' 2>/dev/null)
  cp_init=$(kubectl -n "$NAMESPACE" get rke2controlplane -l cluster.x-k8s.io/cluster-name="$CLUSTER_NAME" -o jsonpath='{.items[0].status.initialized}' 2>/dev/null)

  # Check nodes in the workload cluster
  kc_secret="${CLUSTER_NAME}-kubeconfig"
  workload_nodes_ready=0
  if kubectl -n "$NAMESPACE" get secret "$kc_secret" >/dev/null 2>&1; then
    kubectl -n "$NAMESPACE" get secret "$kc_secret" -o jsonpath='{.data.value}' 2>/dev/null \
      | base64 -d > "/tmp/test-${CLUSTER_NAME}.kc" 2>/dev/null
    if [ -s "/tmp/test-${CLUSTER_NAME}.kc" ]; then
      workload_nodes_ready=$(KUBECONFIG="/tmp/test-${CLUSTER_NAME}.kc" kubectl get nodes --no-headers 2>/dev/null | awk 'BEGIN{n=0} $2=="Ready"{n++} END{print n}')
    fi
  fi
  workload_nodes_ready="${workload_nodes_ready:-0}"

  state="phase=$phase ovhc=$ovhc_ready cp_init=$cp_init nodes_ready=$workload_nodes_ready"
  now=$(date +%s)
  if [ "$state" != "$last_state" ] || [ "$(( now - last_log ))" -ge 60 ]; then
    log_info "[${elapsed}s] $state"
    last_state="$state"
    last_log=$now
  fi

  if [ "$workload_nodes_ready" -ge "2" ]; then
    set -eo pipefail
    pass_test "Cluster $CLUSTER_NAME reached 2 Ready nodes in ${elapsed}s"
    break
  fi

  sleep "$POLL_INTERVAL"
done

set -eo pipefail

# ---- Step 4: cleanup ----
# E2E_KEEP_ON_FAIL=1 lets the next workflow step SSH into the still-running
# OVH instance to grab cloud-init / RKE2 logs. The post-job 'Cleanup OVH
# resources' step in the CI workflow takes care of tearing everything down
# afterwards so we don't leak.
if [ "${TESTS_FAILED:-0}" -gt 0 ] && [ "${E2E_KEEP_ON_FAIL:-0}" = "1" ]; then
  log_warn "E2E_KEEP_ON_FAIL=1 and a test already failed; skipping cluster delete to preserve instance for debug"
  exit 1
fi

log_info "Deleting Cluster $CLUSTER_NAME"
kubectl -n "$NAMESPACE" delete cluster "$CLUSTER_NAME" --wait=false >/dev/null 2>&1 || true

# Wait for all CRs to be gone — same hardening as the readiness loop:
# transient kubectl errors must not abort the whole test.
del_start=$(date +%s)
last_remaining=""
last_log=$del_start
set +eo pipefail

while true; do
  elapsed=$(( $(date +%s) - del_start ))
  if [ "$elapsed" -gt "$TIMEOUT_DELETE" ]; then
    set -eo pipefail
    fail_test "timeout after ${elapsed}s waiting for cluster deletion"
    break
  fi

  remaining=$(kubectl -n "$NAMESPACE" get cluster,ovhcluster,machine -l cluster.x-k8s.io/cluster-name="$CLUSTER_NAME" --no-headers 2>/dev/null | wc -l | tr -d ' ')
  remaining="${remaining:-?}"

  if [ "$remaining" = "0" ]; then
    set -eo pipefail
    pass_test "Cluster $CLUSTER_NAME fully deleted in ${elapsed}s"
    break
  fi

  now=$(date +%s)
  if [ "$remaining" != "$last_remaining" ] || [ "$(( now - last_log ))" -ge 60 ]; then
    log_info "[${elapsed}s] $remaining CAPI resources remaining"
    last_remaining=$remaining
    last_log=$now
  fi
  sleep "$POLL_INTERVAL"
done

set -eo pipefail

# ---- Step 5: verify OVH is clean ----
# Count only resources that are still genuinely live. OVH's FIP reaper is
# async: once we issue DeleteFloatingIP and the FIP transitions to
# status=down/associatedEntity=null, OVH lists it for a few more minutes
# before truly dropping it — that is not a leak from the controller's POV.
inst_count=$(ovh_get "/cloud/project/${OVH_SERVICE_NAME}/instance" | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")
lb_count=$(ovh_get "/cloud/project/${OVH_SERVICE_NAME}/region/${OVH_REGION}/loadbalancing/loadbalancer" | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")
net_count=$(ovh_get "/cloud/project/${OVH_SERVICE_NAME}/network/private" | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")
fip_count=$(ovh_get "/cloud/project/${OVH_SERVICE_NAME}/region/${OVH_REGION}/floatingip" | python3 -c '
import json, sys
fips = json.load(sys.stdin)
# A real leak is a FIP that is still active or still attached. A FIP that
# is "down" with no associatedEntity has been deleted from our side and
# is awaiting async garbage collection by OVH.
live = [f for f in fips if f.get("status") != "down" or f.get("associatedEntity") is not None]
print(len(live))
')
gw_count=$(ovh_get "/cloud/project/${OVH_SERVICE_NAME}/region/${OVH_REGION}/gateway" | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")

log_info "OVH residuals: inst=$inst_count LBs=$lb_count nets=$net_count FIPs=$fip_count gws=$gw_count"

if [ "$inst_count" = "0" ] && [ "$lb_count" = "0" ] && [ "$net_count" = "0" ] && [ "$fip_count" = "0" ] && [ "$gw_count" = "0" ]; then
  pass_test "OVH fully clean after cluster deletion"
else
  fail_test "OVH resources leaked: inst=$inst_count LBs=$lb_count nets=$net_count FIPs=$fip_count gws=$gw_count"
fi

echo "============================================================="
echo "Tests passed:  ${TESTS_PASSED:-0}"
echo "Tests failed:  ${TESTS_FAILED:-0}"
echo "============================================================="

[ "${TESTS_FAILED:-0}" = "0" ]
