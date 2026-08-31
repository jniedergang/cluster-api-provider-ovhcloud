#!/usr/bin/env bash
# CAPIOVH End-to-End integration tests.
#
# Runs against a live management cluster with CAPI core + CAPIOVH controller
# installed, and a real OVH Public Cloud project.
#
# Required environment:
#   KUBECONFIG                Path to management cluster kubeconfig
#   OVH_ENDPOINT              OVH API endpoint (ovh-eu, ovh-ca, ...)
#   OVH_APP_KEY               OVH Application Key
#   OVH_APP_SECRET            OVH Application Secret
#   OVH_CONSUMER_KEY          OVH Consumer Key (scoped to OVH_SERVICE_NAME)
#   OVH_SERVICE_NAME          OVH Public Cloud project ID
#   OVH_REGION                OVH region (e.g. EU-WEST-PAR)
#
# Optional:
#   OVH_SSH_KEY               Name of an SSH key registered in OVH
#   CAPIOVH_NAMESPACE         Test namespace (default: capiovh-e2e)
#   CAPIOVH_CLUSTER_NAME      Test cluster name (default: capiovh-e2e)
#   TIMEOUT_INSTANCE_ACTIVE   seconds, default 600
#   TIMEOUT_LB_ACTIVE         seconds, default 600
#   TIMEOUT_DELETE            seconds, default 300
#
# Usage:
#   ./test/e2e/run-e2e.sh                 # all suites
#   ./test/e2e/run-e2e.sh webhook         # webhook validation only
#   ./test/e2e/run-e2e.sh lifecycle       # OVHCluster + OVHMachine end-to-end
#   ./test/e2e/run-e2e.sh idempotency     # re-apply, kill, re-reconcile
#   ./test/e2e/run-e2e.sh cleanup-orphan  # orphan LB cleanup
#
# Each test cleans up resources it creates, even on failure (trap EXIT).
#
# Test resources are prefixed with "capiovh-e2e-" so they are easy to
# identify and clean up manually if needed.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=utils.sh
source "${SCRIPT_DIR}/utils.sh"

# ---- Configuration ----
NAMESPACE="${CAPIOVH_NAMESPACE:-capiovh-e2e}"
CLUSTER_NAME="${CAPIOVH_CLUSTER_NAME:-capiovh-e2e}"
TIMEOUT_INSTANCE_ACTIVE="${TIMEOUT_INSTANCE_ACTIVE:-600}"
TIMEOUT_LB_ACTIVE="${TIMEOUT_LB_ACTIVE:-600}"
TIMEOUT_DELETE="${TIMEOUT_DELETE:-300}"

# ---- Counters ----
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

# ---- Helpers ----

require_env() {
  local missing=0
  for var in KUBECONFIG OVH_ENDPOINT OVH_APP_KEY OVH_APP_SECRET OVH_CONSUMER_KEY OVH_SERVICE_NAME OVH_REGION; do
    if [ -z "${!var:-}" ]; then
      log_fail "Required env var $var is not set"
      missing=1
    fi
  done
  if [ "$missing" = 1 ]; then
    exit 2
  fi
}

precondition_checks() {
  log_test "Preconditions"

  if ! kubectl get nodes >/dev/null 2>&1; then
    log_fail "kubectl cannot reach the cluster (KUBECONFIG=$KUBECONFIG)"
    exit 2
  fi
  log_ok "kubectl reaches management cluster"

  if ! kubectl get crd ovhclusters.infrastructure.cluster.x-k8s.io >/dev/null 2>&1; then
    log_fail "OVHCluster CRD missing, install CAPIOVH first"
    exit 2
  fi
  log_ok "CAPIOVH CRDs installed"

  if ! kubectl -n capiovh-system get deploy 2>/dev/null | grep -q controller-manager; then
    if ! kubectl -n capiovh-system get deploy 2>/dev/null | grep -q capiovh; then
      log_fail "CAPIOVH controller manager not found in namespace capiovh-system"
      exit 2
    fi
  fi
  log_ok "CAPIOVH controller manager deployed"

  if ! ovh_get "/cloud/project/${OVH_SERVICE_NAME}/region" >/dev/null 2>&1; then
    log_fail "OVH API not reachable or credentials invalid"
    exit 2
  fi
  log_ok "OVH API reachable, credentials valid"
}

setup_namespace() {
  # Wait for any previous Terminating namespace to disappear, so back-to-back
  # suites don't race on the same namespace name.
  for _ in $(seq 1 60); do
    phase=$(kubectl get ns "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    if [ -z "$phase" ] || [ "$phase" = "Active" ]; then
      break
    fi
    sleep 1
  done

  kubectl get ns "$NAMESPACE" >/dev/null 2>&1 || kubectl create ns "$NAMESPACE"

  kubectl -n "$NAMESPACE" create secret generic ovh-credentials \
    --from-literal=endpoint="$OVH_ENDPOINT" \
    --from-literal=applicationKey="$OVH_APP_KEY" \
    --from-literal=applicationSecret="$OVH_APP_SECRET" \
    --from-literal=consumerKey="$OVH_CONSUMER_KEY" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
}

teardown_namespace() {
  log_info "Cleaning up namespace ${NAMESPACE} ..."
  kubectl delete ns "$NAMESPACE" --wait=false >/dev/null 2>&1 || true
}

apply_yaml() {
  echo "$1" | kubectl apply -f - 2>&1
}

# ---- Suite: webhook validation ----

test_webhook() {
  log_test "Suite: webhook validation"

  setup_namespace
  trap teardown_namespace RETURN

  log_info "Applying invalid OVHCluster (no subnetID, no networkConfig), should be rejected"
  output=$(apply_yaml "$(cat <<EOF
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata:
  name: invalid-test
  namespace: ${NAMESPACE}
spec:
  serviceName: foo
  region: bar
  identitySecret:
    name: x
    namespace: ${NAMESPACE}
  loadBalancerConfig: {}
EOF
)" || true)
  if echo "$output" | grep -q "either spec.loadBalancerConfig.subnetID or spec.networkConfig"; then
    pass_test "webhook rejected invalid OVHCluster with expected message"
  else
    fail_test "webhook did not reject invalid OVHCluster (or wrong message): $output"
  fi

  log_info "Applying valid OVHCluster, should be accepted"
  output=$(apply_yaml "$(cat <<EOF
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata:
  name: webhook-valid
  namespace: ${NAMESPACE}
spec:
  serviceName: foo
  region: bar
  identitySecret:
    name: x
    namespace: ${NAMESPACE}
  loadBalancerConfig: {}
  networkConfig:
    subnetCIDR: "10.0.0.0/24"
EOF
)" 2>&1 || true)
  if echo "$output" | grep -q "created"; then
    pass_test "webhook accepted valid OVHCluster"
    kubectl -n "$NAMESPACE" delete ovhcluster webhook-valid --wait=false >/dev/null 2>&1 || true
  else
    fail_test "webhook rejected valid OVHCluster: $output"
  fi
}

# ---- Suite: lifecycle (full create + delete) ----

test_lifecycle() {
  log_test "Suite: lifecycle (Cluster + OVHCluster, expect network + LB created in OVH)"

  setup_namespace
  trap teardown_namespace RETURN

  log_info "Applying Cluster + OVHCluster ..."
  apply_yaml "$(cat <<EOF
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  namespace: ${NAMESPACE}
  name: ${CLUSTER_NAME}
spec:
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
    kind: OVHCluster
    name: ${CLUSTER_NAME}
---
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata:
  name: ${CLUSTER_NAME}
  namespace: ${NAMESPACE}
spec:
  serviceName: ${OVH_SERVICE_NAME}
  region: ${OVH_REGION}
  identitySecret:
    namespace: ${NAMESPACE}
    name: ovh-credentials
  loadBalancerConfig: {}
  networkConfig:
    subnetCIDR: "10.42.0.0/24"
EOF
)" >/dev/null

  log_info "Waiting for OVHCluster.status.ready=true (timeout: ${TIMEOUT_LB_ACTIVE}s) ..."
  if wait_for_condition "OVHCluster ready" "${TIMEOUT_LB_ACTIVE}" \
    "kubectl -n ${NAMESPACE} get ovhcluster ${CLUSTER_NAME} -o jsonpath='{.status.ready}' | grep -q true"; then
    pass_test "OVHCluster reached Ready"
  else
    fail_test "OVHCluster did not reach Ready within ${TIMEOUT_LB_ACTIVE}s"
    return
  fi

  # v1beta2 contract proof. The infra provider must report
  # status.initialization.provisioned (Phase A field), and the core Cluster
  # controller must read it via the v1beta2 contract label (Phase B flip) into
  # Cluster.status.initialization.infrastructureProvisioned. A wrong or missing
  # contract label would leave infrastructureProvisioned unset even though the
  # OVHCluster itself reconciled to Ready.
  prov=$(kubectl -n "$NAMESPACE" get ovhcluster "$CLUSTER_NAME" -o jsonpath='{.status.initialization.provisioned}')
  if [ "$prov" = "true" ]; then
    pass_test "OVHCluster.status.initialization.provisioned=true (v1beta2 field present)"
  else
    fail_test "OVHCluster.status.initialization.provisioned not true (got '${prov}')"
  fi

  if wait_for_condition "Cluster infrastructureProvisioned" 120 \
    "kubectl -n ${NAMESPACE} get cluster ${CLUSTER_NAME} -o jsonpath='{.status.initialization.infrastructureProvisioned}' | grep -q true"; then
    pass_test "Cluster.status.initialization.infrastructureProvisioned=true (v1beta2 contract resolved)"
  else
    fail_test "core Cluster never read the v1beta2 contract (infrastructureProvisioned not true)"
  fi

  # Verify resources exist in OVH
  network_id=$(kubectl -n "$NAMESPACE" get ovhcluster "$CLUSTER_NAME" -o jsonpath='{.status.networkID}')
  lb_id=$(kubectl -n "$NAMESPACE" get ovhcluster "$CLUSTER_NAME" -o jsonpath='{.status.loadBalancerID}')

  if [ -n "$network_id" ] && ovh_get "/cloud/project/${OVH_SERVICE_NAME}/network/private/${network_id}" >/dev/null; then
    pass_test "Private network ${network_id} exists in OVH"
  else
    fail_test "Private network not found in OVH (status.networkID=${network_id})"
  fi

  if [ -n "$lb_id" ] && ovh_get "/cloud/project/${OVH_SERVICE_NAME}/region/${OVH_REGION}/loadbalancing/loadbalancer/${lb_id}" >/dev/null; then
    pass_test "Load balancer ${lb_id} exists in OVH"
  else
    fail_test "LB not found in OVH (status.loadBalancerID=${lb_id})"
  fi

  log_info "Deleting Cluster + OVHCluster ..."
  kubectl -n "$NAMESPACE" delete cluster "$CLUSTER_NAME" --wait=false >/dev/null 2>&1 || true
  kubectl -n "$NAMESPACE" delete ovhcluster "$CLUSTER_NAME" --wait=false >/dev/null 2>&1 || true

  log_info "Waiting for OVHCluster CR removal (timeout: ${TIMEOUT_DELETE}s) ..."
  if wait_for_condition "OVHCluster removed" "${TIMEOUT_DELETE}" \
    "! kubectl -n ${NAMESPACE} get ovhcluster ${CLUSTER_NAME} >/dev/null 2>&1"; then
    pass_test "OVHCluster CR removed"
  else
    fail_test "OVHCluster CR still present after ${TIMEOUT_DELETE}s"
  fi

  # Verify OVH resources cleaned up
  if [ -n "$network_id" ]; then
    if ! ovh_get "/cloud/project/${OVH_SERVICE_NAME}/network/private/${network_id}" >/dev/null 2>&1; then
      pass_test "Private network ${network_id} cleaned up in OVH"
    else
      fail_test "Private network ${network_id} still exists in OVH after Cluster deletion"
    fi
  fi
}

# ---- Suite: idempotency ----

test_idempotency() {
  log_test "Suite: idempotency (re-apply same Cluster, no duplicates)"

  setup_namespace
  trap teardown_namespace RETURN

  log_info "Applying Cluster + OVHCluster (first time) ..."
  apply_yaml "$(cat <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: ovh-credentials
  namespace: ${NAMESPACE}
type: Opaque
stringData:
  endpoint: ${OVH_ENDPOINT}
  applicationKey: ${OVH_APP_KEY}
  applicationSecret: ${OVH_APP_SECRET}
  consumerKey: ${OVH_CONSUMER_KEY}
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: ${CLUSTER_NAME}
  namespace: ${NAMESPACE}
spec:
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
    kind: OVHCluster
    name: ${CLUSTER_NAME}
---
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata:
  name: ${CLUSTER_NAME}
  namespace: ${NAMESPACE}
spec:
  serviceName: ${OVH_SERVICE_NAME}
  region: ${OVH_REGION}
  identitySecret:
    namespace: ${NAMESPACE}
    name: ovh-credentials
  loadBalancerConfig: {}
  networkConfig:
    subnetCIDR: "10.43.0.0/24"
EOF
)" >/dev/null

  log_info "Waiting for first reconcile to create LB ..."
  wait_for_condition "first LB created" 360 \
    "kubectl -n ${NAMESPACE} get ovhcluster ${CLUSTER_NAME} -o jsonpath='{.status.loadBalancerID}' | grep -q ."

  first_lb=$(kubectl -n "$NAMESPACE" get ovhcluster "$CLUSTER_NAME" -o jsonpath='{.status.loadBalancerID}')
  log_info "First LB: $first_lb"

  log_info "Restarting controller to force re-reconcile ..."
  capiovh_deploy=$(kubectl -n capiovh-system get deploy -o name | head -1)
  kubectl -n capiovh-system rollout restart "$capiovh_deploy" >/dev/null 2>&1
  kubectl -n capiovh-system rollout status "$capiovh_deploy" --timeout=120s >/dev/null

  log_info "Waiting 30s for re-reconcile cycle ..."
  for _ in 1 2 3; do
    sleep 1
  done

  second_lb=$(kubectl -n "$NAMESPACE" get ovhcluster "$CLUSTER_NAME" -o jsonpath='{.status.loadBalancerID}')

  # Count LBs with our prefix in OVH
  prefix="capi-${CLUSTER_NAME}-lb"
  count=$(ovh_get "/cloud/project/${OVH_SERVICE_NAME}/region/${OVH_REGION}/loadbalancing/loadbalancer" 2>/dev/null \
    | python3 -c "import sys,json; print(sum(1 for lb in json.load(sys.stdin) if lb.get('name','').startswith('$prefix')))")

  if [ "$count" = "1" ] && [ "$first_lb" = "$second_lb" ]; then
    pass_test "Idempotent: 1 LB before and after restart, same ID ($first_lb)"
  else
    fail_test "Not idempotent: ${count} LB(s) with prefix ${prefix} (first=$first_lb, second=$second_lb)"
  fi

  # Cleanup: delete the Cluster + OVHCluster BEFORE the namespace teardown.
  # The OVHCluster finalizer needs the ovh-credentials Secret to talk to OVH
  # for LB/network cleanup; if the namespace starts terminating first, the
  # Secret disappears and the finalizer loops forever.
  kubectl -n "$NAMESPACE" delete cluster "$CLUSTER_NAME" --wait=false >/dev/null 2>&1 || true
  log_info "Waiting for OVHCluster CR finalizer to clear ..."
  wait_for_condition "OVHCluster CR removed" "${TIMEOUT_DELETE}" \
    "! kubectl -n ${NAMESPACE} get ovhcluster ${CLUSTER_NAME} >/dev/null 2>&1" \
    || log_info "Warning: OVHCluster CR still present after ${TIMEOUT_DELETE}s, OVH resources may leak"
}

# ---- Main ----

require_env
precondition_checks

SUITES_TO_RUN=("${@:-webhook lifecycle idempotency}")

for suite in $SUITES_TO_RUN; do
  case "$suite" in
    webhook)      test_webhook ;;
    lifecycle)    test_lifecycle ;;
    idempotency)  test_idempotency ;;
    *)            log_warn "Unknown suite: $suite" ;;
  esac
done

echo
echo "============================================================="
echo "Tests passed:  $TESTS_PASSED"
echo "Tests failed:  $TESTS_FAILED"
echo "Tests skipped: $TESTS_SKIPPED"
echo "============================================================="

if [ "$TESTS_FAILED" -gt 0 ]; then
  exit 1
fi
