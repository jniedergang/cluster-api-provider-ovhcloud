#!/bin/bash
# Webhook + CRD validation negative cases for CAPIOVH
# Each test creates an invalid CR via kubectl apply --dry-run=server.
# Validation can reject at either layer (CRD schema or webhook), and we
# accept either path — both protect the user.
set -u

KC="ssh -o BatchMode=yes rancher@172.16.3.20 sudo /var/lib/rancher/rke2/bin/kubectl --kubeconfig /etc/rancher/rke2/rke2.yaml -n fleet-default apply --dry-run=server -f -"

PASS=0
FAIL=0

assert_reject() {
  local label="$1"
  local manifest="$2"
  local want_msg="$3"
  out=$(echo "$manifest" | $KC 2>&1)
  if echo "$out" | grep -qE "$want_msg"; then
    echo "✅ $label"
    PASS=$((PASS+1))
  else
    echo "❌ $label"
    echo "    Wanted match: $want_msg"
    echo "    Got: $(echo "$out" | tail -1)"
    FAIL=$((FAIL+1))
  fi
}

assert_accept() {
  local label="$1"
  local manifest="$2"
  out=$(echo "$manifest" | $KC 2>&1)
  if echo "$out" | grep -qE 'configured \(server dry run\)|created \(server dry run\)'; then
    echo "✅ $label"
    PASS=$((PASS+1))
  else
    echo "❌ $label (unexpectedly rejected): $out" | head -2
    FAIL=$((FAIL+1))
  fi
}

echo "=== OVHCluster — required fields (CRD-level) ==="

assert_reject "missing serviceName" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata: {name: bad1, namespace: fleet-default}
spec:
  region: EU-WEST-PAR
  identitySecret: {name: ovh-credentials, namespace: fleet-default}
  loadBalancerConfig: {}
  networkConfig: {subnetCIDR: 10.50.0.0/24}
' 'spec\.serviceName.*[Rr]equired'

assert_reject "missing region" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata: {name: bad2, namespace: fleet-default}
spec:
  serviceName: f0c6e585c9e3495d901ef3e67f276314
  identitySecret: {name: ovh-credentials, namespace: fleet-default}
  loadBalancerConfig: {}
  networkConfig: {subnetCIDR: 10.50.0.0/24}
' 'spec\.region.*[Rr]equired'

assert_reject "missing identitySecret" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata: {name: bad3, namespace: fleet-default}
spec:
  serviceName: f0c6e585c9e3495d901ef3e67f276314
  region: EU-WEST-PAR
  loadBalancerConfig: {}
  networkConfig: {subnetCIDR: 10.50.0.0/24}
' 'spec\.identitySecret.*[Rr]equired'

assert_reject "missing loadBalancerConfig" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata: {name: bad4, namespace: fleet-default}
spec:
  serviceName: f0c6e585c9e3495d901ef3e67f276314
  region: EU-WEST-PAR
  identitySecret: {name: ovh-credentials, namespace: fleet-default}
  networkConfig: {subnetCIDR: 10.50.0.0/24}
' 'spec\.loadBalancerConfig.*[Rr]equired'

echo
echo "=== OVHCluster — webhook-level validation ==="

assert_reject "no subnet AND no networkConfig (webhook)" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata: {name: bad5, namespace: fleet-default}
spec:
  serviceName: f0c6e585c9e3495d901ef3e67f276314
  region: EU-WEST-PAR
  identitySecret: {name: ovh-credentials, namespace: fleet-default}
  loadBalancerConfig: {}
' 'either spec.loadBalancerConfig.subnetID or spec.networkConfig'

assert_reject "networkConfig with no privateNetworkID nor subnetCIDR (webhook)" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata: {name: bad6, namespace: fleet-default}
spec:
  serviceName: f0c6e585c9e3495d901ef3e67f276314
  region: EU-WEST-PAR
  identitySecret: {name: ovh-credentials, namespace: fleet-default}
  loadBalancerConfig: {}
  networkConfig: {gateway: 10.50.0.1}
' 'requires either privateNetworkID or subnetCIDR|subnetCIDR.*[Rr]equired'

assert_reject "networkConfig.gateway not an IP (webhook)" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata: {name: bad7, namespace: fleet-default}
spec:
  serviceName: f0c6e585c9e3495d901ef3e67f276314
  region: EU-WEST-PAR
  identitySecret: {name: ovh-credentials, namespace: fleet-default}
  loadBalancerConfig: {}
  networkConfig: {subnetCIDR: 10.50.0.0/24, gateway: not-an-ip}
' 'is not a valid IP address'

echo
echo "=== OVHCluster — CRD bound checks (vlanID) ==="

assert_reject "vlanID > 4094" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata: {name: bad8, namespace: fleet-default}
spec:
  serviceName: f0c6e585c9e3495d901ef3e67f276314
  region: EU-WEST-PAR
  identitySecret: {name: ovh-credentials, namespace: fleet-default}
  loadBalancerConfig: {}
  networkConfig: {subnetCIDR: 10.50.0.0/24, vlanID: 9999}
' 'should be less than or equal to 4094'

assert_reject "vlanID < 0" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata: {name: bad9, namespace: fleet-default}
spec:
  serviceName: f0c6e585c9e3495d901ef3e67f276314
  region: EU-WEST-PAR
  identitySecret: {name: ovh-credentials, namespace: fleet-default}
  loadBalancerConfig: {}
  networkConfig: {subnetCIDR: 10.50.0.0/24, vlanID: -5}
' 'should be greater than or equal to 0'

echo
echo "=== OVHMachine — required fields (CRD-level) ==="

assert_reject "missing flavorName" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHMachine
metadata: {name: m-bad1, namespace: fleet-default}
spec:
  imageName: Ubuntu 22.04
' 'spec\.flavorName.*[Rr]equired'

assert_reject "missing imageName" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHMachine
metadata: {name: m-bad2, namespace: fleet-default}
spec:
  flavorName: b3-8
' 'spec\.imageName.*[Rr]equired'

echo
echo "=== OVHMachine — webhook + CRD bound checks ==="

assert_reject "rootDiskSize negative (webhook)" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHMachine
metadata: {name: m-bad3, namespace: fleet-default}
spec:
  flavorName: b3-8
  imageName: Ubuntu 22.04
  rootDiskSize: -1
' 'rootDiskSize must be >= 0'

assert_reject "additionalVolume missing name" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHMachine
metadata: {name: m-bad4, namespace: fleet-default}
spec:
  flavorName: b3-8
  imageName: Ubuntu 22.04
  additionalVolumes:
  - sizeGB: 10
' 'additionalVolumes\[0\]\.name.*[Rr]equired'

assert_reject "additionalVolume sizeGB < 1" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHMachine
metadata: {name: m-bad5, namespace: fleet-default}
spec:
  flavorName: b3-8
  imageName: Ubuntu 22.04
  additionalVolumes:
  - {name: data, sizeGB: 0}
' 'should be greater than or equal to 1|sizeGB must be >= 1'

echo
echo "=== Sanity: VALID specs are accepted ==="

assert_accept "valid OVHCluster with vlanID=200" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHCluster
metadata: {name: good1, namespace: fleet-default}
spec:
  serviceName: f0c6e585c9e3495d901ef3e67f276314
  region: EU-WEST-PAR
  identitySecret: {name: ovh-credentials, namespace: fleet-default}
  loadBalancerConfig: {}
  networkConfig: {subnetCIDR: 10.50.0.0/24, vlanID: 200}
'

assert_accept "valid OVHMachine" '
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: OVHMachine
metadata: {name: m-good1, namespace: fleet-default}
spec:
  flavorName: b3-8
  imageName: Ubuntu 22.04
  rootDiskSize: 50
  additionalVolumes:
  - {name: data, sizeGB: 100}
'

echo
echo "==========================================="
echo "PASS: $PASS  FAIL: $FAIL"
echo "==========================================="
exit $FAIL
