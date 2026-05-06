#!/usr/bin/env bash
# When the E2E lifecycle test fails with a CP node provisioned but never
# Ready, this script grabs cloud-init / RKE2 logs from the running OVH
# instance(s) so we can diagnose what cloud-init actually did.
#
# Required env:
#   OVH_ENDPOINT, OVH_APP_KEY, OVH_APP_SECRET, OVH_CONSUMER_KEY
#   OVH_SERVICE_NAME, OVH_REGION (default EU-WEST-PAR)
#   OVH_SSH_PRIVATE_KEY (private key matching OVH_SSH_KEY public key)

set -uo pipefail

: "${OVH_ENDPOINT:?required}"
: "${OVH_APP_KEY:?required}"
: "${OVH_APP_SECRET:?required}"
: "${OVH_CONSUMER_KEY:?required}"
: "${OVH_SERVICE_NAME:?required}"
: "${OVH_SSH_PRIVATE_KEY:?required}"
OVH_REGION="${OVH_REGION:-EU-WEST-PAR}"

case "$OVH_ENDPOINT" in
  ovh-eu) EP_HOST=eu;;
  ovh-ca) EP_HOST=ca;;
  ovh-us) EP_HOST=us;;
  *) EP_HOST=$OVH_ENDPOINT;;
esac

ovh_get() {
  local path=$1 ts sig
  ts=$(curl -fsS "https://${EP_HOST}.api.ovh.com/1.0/auth/time")
  sig="\$1\$$(printf '%s' "${OVH_APP_SECRET}+${OVH_CONSUMER_KEY}+GET+https://${EP_HOST}.api.ovh.com/1.0${path}++${ts}" | sha1sum | cut -d' ' -f1)"
  curl -fsS "https://${EP_HOST}.api.ovh.com/1.0${path}" \
    -H "X-Ovh-Application: $OVH_APP_KEY" -H "X-Ovh-Consumer: $OVH_CONSUMER_KEY" \
    -H "X-Ovh-Timestamp: $ts" -H "X-Ovh-Signature: $sig"
}

OUT=/tmp/instance-logs
mkdir -p "$OUT"

echo "[collect] listing instances"
ovh_get "/cloud/project/$OVH_SERVICE_NAME/instance" \
  | python3 -c '
import json, sys
for inst in json.load(sys.stdin):
  pub = next((a["ip"] for a in inst.get("ipAddresses", [])
              if a.get("type") == "public" and a.get("version") == 4), "")
  if pub and inst.get("status") == "ACTIVE":
    print(inst["id"], inst["name"], pub)
' > "$OUT/instances.txt"
cat "$OUT/instances.txt"

if [ ! -s "$OUT/instances.txt" ]; then
  echo "[collect] no ACTIVE instance with public IP, nothing to collect"
  exit 0
fi

KEY=$(mktemp)
chmod 600 "$KEY"
trap 'rm -f "$KEY"' EXIT
printf '%s' "$OVH_SSH_PRIVATE_KEY" > "$KEY"

while read -r id name ip; do
  echo "[collect] SSH ${name} (${ip})"
  for try in 1 2 3 4 5; do
    if ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
           -o ConnectTimeout=10 -o BatchMode=yes \
           -i "$KEY" "ubuntu@${ip}" 'true' 2>/dev/null; then
      break
    fi
    sleep 5
  done

  ssh_run() {
    ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        -o ConnectTimeout=10 -i "$KEY" "ubuntu@${ip}" "$@" 2>&1
  }

  {
    echo "=== ${name} ${ip} ==="
    echo "--- cloud-init status ---"
    ssh_run 'sudo cloud-init status --long'
    echo "--- /var/log/cloud-init-output.log (last 200) ---"
    ssh_run 'sudo tail -n 200 /var/log/cloud-init-output.log'
    echo "--- /var/log/cloud-init.log (last 100) ---"
    ssh_run 'sudo tail -n 100 /var/log/cloud-init.log'
    echo "--- /etc/rancher/rke2/config.yaml ---"
    ssh_run 'sudo cat /etc/rancher/rke2/config.yaml 2>/dev/null || echo "missing"'
    echo "--- systemctl status rke2-server (or rke2-agent) ---"
    ssh_run 'sudo systemctl status rke2-server 2>&1 | head -50; sudo systemctl status rke2-agent 2>&1 | head -50'
    echo "--- journalctl rke2-server (last 100) ---"
    ssh_run 'sudo journalctl -u rke2-server --no-pager -n 100 2>&1'
    echo "--- journalctl rke2-agent (last 100) ---"
    ssh_run 'sudo journalctl -u rke2-agent --no-pager -n 100 2>&1'
    echo "--- listening ports ---"
    ssh_run 'sudo ss -tlnp 2>&1 | head'
    echo "--- /var/lib/cloud/instance/user-data.txt (head) ---"
    ssh_run 'sudo head -c 4000 /var/lib/cloud/instance/user-data.txt 2>&1'
    echo
  } > "$OUT/${name}.log" 2>&1
  echo "[collect] wrote $OUT/${name}.log ($(wc -l < "$OUT/${name}.log") lines)"
done < "$OUT/instances.txt"

echo "[collect] done"
