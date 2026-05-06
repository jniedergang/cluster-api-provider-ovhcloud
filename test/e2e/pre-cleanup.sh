#!/usr/bin/env bash
# Wipe all CAPIOVH-created resources from the OVH test project before a run.
# Idempotent. Safe to call when the project is already empty.
#
# Required env:
#   OVH_ENDPOINT, OVH_APP_KEY, OVH_APP_SECRET, OVH_CONSUMER_KEY, OVH_SERVICE_NAME
#   OVH_REGION (default EU-WEST-PAR)
#   OVH_OS_USERNAME, OVH_OS_PASSWORD (OpenStack admin user; needed for router cleanup)

set -euo pipefail

: "${OVH_ENDPOINT:?required}"
: "${OVH_APP_KEY:?required}"
: "${OVH_APP_SECRET:?required}"
: "${OVH_CONSUMER_KEY:?required}"
: "${OVH_SERVICE_NAME:?required}"
: "${OVH_OS_USERNAME:?required}"
: "${OVH_OS_PASSWORD:?required}"
OVH_REGION="${OVH_REGION:-EU-WEST-PAR}"

case "$OVH_ENDPOINT" in
  ovh-eu) EP_HOST=eu;;
  ovh-ca) EP_HOST=ca;;
  ovh-us) EP_HOST=us;;
  *) EP_HOST=$OVH_ENDPOINT;;
esac

ovh() {
  local method=$1 path=$2
  local ts sig sig_input
  ts=$(curl -fsS "https://${EP_HOST}.api.ovh.com/1.0/auth/time")
  sig_input="${OVH_APP_SECRET}+${OVH_CONSUMER_KEY}+${method}+https://${EP_HOST}.api.ovh.com/1.0${path}++${ts}"
  sig="\$1\$$(printf '%s' "$sig_input" | sha1sum | cut -d' ' -f1)"
  curl -fsS -X "$method" "https://${EP_HOST}.api.ovh.com/1.0${path}" \
    -H "X-Ovh-Application: $OVH_APP_KEY" \
    -H "X-Ovh-Consumer: $OVH_CONSUMER_KEY" \
    -H "X-Ovh-Timestamp: $ts" \
    -H "X-Ovh-Signature: $sig" \
    -H "Content-Type: application/json" || true
}

log() { echo "[pre-cleanup] $*"; }

# --- 1. Delete all instances ---
log "Step 1: instances"
for id in $(ovh GET "/cloud/project/$OVH_SERVICE_NAME/instance" | python3 -c 'import json,sys; [print(i["id"]) for i in json.load(sys.stdin) if i.get("region")==r]' r="$OVH_REGION" 2>/dev/null || true); do
  log "  delete instance $id"
  ovh DELETE "/cloud/project/$OVH_SERVICE_NAME/instance/$id" >/dev/null
done

# --- 2. Delete all load balancers (async) ---
# OVH refuses DELETE on LBs in PENDING_CREATE / PENDING_DELETE; we skip them
# and let the per-project quota absorb the orphan. Fresh test runs use
# distinct LB names so a stuck LB does not block a new one.
log "Step 2: load balancers"
ovh GET "/cloud/project/$OVH_SERVICE_NAME/region/$OVH_REGION/loadbalancing/loadbalancer" \
  | python3 -c '
import json, sys
for l in json.load(sys.stdin):
  status = l.get("provisioningStatus", "")
  print(l["id"], status)
' > /tmp/_lbs.txt
while read -r id status; do
  if [ -z "$id" ]; then continue; fi
  case "$status" in
    PENDING_*|creating)
      log "  skip stuck LB $id ($status) — orphan, OVH will reap eventually"
      ;;
    *)
      log "  delete LB $id ($status)"
      ovh DELETE "/cloud/project/$OVH_SERVICE_NAME/region/$OVH_REGION/loadbalancing/loadbalancer/$id" >/dev/null
      ;;
  esac
done < /tmp/_lbs.txt
LB_IDS=$(awk '$2 !~ /^PENDING_/ && $2 != "creating" {print $1}' /tmp/_lbs.txt)

# --- 3. Wait for LB deletion (releases the FIPs and routers) ---
if [ -n "$LB_IDS" ]; then
  log "  waiting up to 90s for LBs to disappear"
  for _ in $(seq 1 18); do
    remaining=$(ovh GET "/cloud/project/$OVH_SERVICE_NAME/region/$OVH_REGION/loadbalancing/loadbalancer" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)))' 2>/dev/null || echo "?")
    [ "$remaining" = "0" ] && break
    sleep 5
  done
  log "  LBs remaining: $remaining"
fi

# --- 4. Delete all floating IPs ---
log "Step 3: floating IPs"
for id in $(ovh GET "/cloud/project/$OVH_SERVICE_NAME/region/$OVH_REGION/floatingip" | python3 -c 'import json,sys; [print(f["id"]) for f in json.load(sys.stdin)]' 2>/dev/null || true); do
  log "  delete FIP $id"
  ovh DELETE "/cloud/project/$OVH_SERVICE_NAME/region/$OVH_REGION/floatingip/$id" >/dev/null
done

# --- 5. Detach + delete CAPI-created routers (visible only via OpenStack) ---
log "Step 4: routers (via openstack)"
export OS_AUTH_URL=https://auth.cloud.ovh.net/v3
export OS_USERNAME=$OVH_OS_USERNAME
export OS_PASSWORD=$OVH_OS_PASSWORD
export OS_PROJECT_ID=$OVH_SERVICE_NAME
export OS_REGION_NAME=$OVH_REGION
export OS_IDENTITY_API_VERSION=3
export OS_PROJECT_DOMAIN_NAME=Default
export OS_USER_DOMAIN_NAME=Default
export OS_INTERFACE=public

if command -v openstack >/dev/null 2>&1; then
  for router in $(openstack router list -f value -c ID -c Name 2>/dev/null | awk '/capi-/{print $1}'); do
    log "  detaching subnets from router $router"
    openstack port list --router "$router" -f json 2>/dev/null \
      | python3 -c '
import json, sys, re
ports = json.load(sys.stdin)
seen = set()
for p in ports:
  fips = p.get("Fixed IP Addresses", [])
  for fip in fips if isinstance(fips, list) else []:
    sid = fip.get("subnet_id") if isinstance(fip, dict) else None
    if sid and sid not in seen:
      seen.add(sid)
      print(sid)
' | while read -r subnet; do
        openstack router remove subnet "$router" "$subnet" 2>&1 || true
      done
    log "  deleting router $router"
    openstack router delete "$router" 2>&1 || true
  done
else
  log "  openstack CLI not installed, skipping router cleanup"
fi

# --- 6. Delete all private networks ---
log "Step 5: private networks"
for id in $(ovh GET "/cloud/project/$OVH_SERVICE_NAME/network/private" | python3 -c 'import json,sys; [print(n["id"]) for n in json.load(sys.stdin)]' 2>/dev/null || true); do
  log "  delete network $id"
  ovh DELETE "/cloud/project/$OVH_SERVICE_NAME/network/private/$id" >/dev/null
done

# --- 7. Final state ---
log "Final state:"
log "  instances: $(ovh GET /cloud/project/$OVH_SERVICE_NAME/instance | python3 -c 'import json,sys;print(len(json.load(sys.stdin)))' 2>/dev/null || echo '?')"
log "  LBs: $(ovh GET /cloud/project/$OVH_SERVICE_NAME/region/$OVH_REGION/loadbalancing/loadbalancer | python3 -c 'import json,sys;print(len(json.load(sys.stdin)))' 2>/dev/null || echo '?')"
log "  FIPs: $(ovh GET /cloud/project/$OVH_SERVICE_NAME/region/$OVH_REGION/floatingip | python3 -c 'import json,sys;print(len(json.load(sys.stdin)))' 2>/dev/null || echo '?')"
log "  networks: $(ovh GET /cloud/project/$OVH_SERVICE_NAME/network/private | python3 -c 'import json,sys;print(len(json.load(sys.stdin)))' 2>/dev/null || echo '?')"
log "done"
