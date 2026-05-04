#!/usr/bin/env bash
# Shared helpers for CAPIOVH E2E test scripts.

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC}  $(date +%H:%M:%S) $*"; }
log_ok()   { echo -e "${GREEN}[PASS]${NC}  $(date +%H:%M:%S) $*"; }
log_fail() { echo -e "${RED}[FAIL]${NC}  $(date +%H:%M:%S) $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC}  $(date +%H:%M:%S) $*"; }
log_test() {
  echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BLUE}[TEST]${NC}  $*"
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

# Test counters — initialised here so callers using `set -u` don't trip.
: "${TESTS_PASSED:=0}"
: "${TESTS_FAILED:=0}"
: "${TESTS_SKIPPED:=0}"

pass_test() { TESTS_PASSED=$((TESTS_PASSED + 1)); log_ok "$1"; }
fail_test() { TESTS_FAILED=$((TESTS_FAILED + 1)); log_fail "$1"; }
skip_test() { TESTS_SKIPPED=$((TESTS_SKIPPED + 1)); log_warn "SKIP: $1"; }

# Wait for a shell command to succeed within `timeout` seconds.
# Returns 0 on success, 1 on timeout.
wait_for_condition() {
  local desc="$1"
  local timeout="$2"
  local cmd="$3"
  local start end
  start=$(date +%s)
  end=$((start + timeout))

  log_info "Waiting for: $desc (timeout: ${timeout}s)"

  while true; do
    if eval "$cmd" >/dev/null 2>&1; then
      return 0
    fi
    if [ "$(date +%s)" -gt "$end" ]; then
      log_warn "Timeout waiting for: $desc"
      return 1
    fi
    sleep 5
  done
}

# Compute the OVH HMAC-SHA1 signature and call the API directly.
# Args: $1=method, $2=path (e.g. /cloud/project/abc/region)
# Reads body from stdin if method is POST/PUT.
# Echoes the response body. Exit code: HTTP status not 2xx -> non-zero exit.
ovh_call() {
  local method="$1"
  local path="$2"
  local base
  case "$OVH_ENDPOINT" in
    ovh-eu)        base="https://eu.api.ovh.com/1.0" ;;
    ovh-ca)        base="https://ca.api.ovh.com/1.0" ;;
    ovh-us)        base="https://api.us.ovhcloud.com/1.0" ;;
    *)             base="https://${OVH_ENDPOINT}/1.0" ;;
  esac

  local now sig body
  now=$(curl -fsS "${base}/auth/time")

  if [[ "$method" =~ ^(POST|PUT)$ ]]; then
    body=$(cat)
  else
    body=""
  fi

  # OVH signature: $1$ + sha1(AS+CK+METHOD+URL+BODY+TIMESTAMP)
  sig="\$1\$$(printf '%s+%s+%s+%s%s+%s+%s' "$OVH_APP_SECRET" "$OVH_CONSUMER_KEY" "$method" "$base" "$path" "$body" "$now" | sha1sum | awk '{print $1}')"

  if [[ "$method" =~ ^(POST|PUT)$ ]]; then
    curl -fsS -X "$method" "${base}${path}" \
      -H "X-Ovh-Application: $OVH_APP_KEY" \
      -H "X-Ovh-Consumer: $OVH_CONSUMER_KEY" \
      -H "X-Ovh-Timestamp: $now" \
      -H "X-Ovh-Signature: $sig" \
      -H "Content-Type: application/json" \
      -d "$body"
  else
    curl -fsS -X "$method" "${base}${path}" \
      -H "X-Ovh-Application: $OVH_APP_KEY" \
      -H "X-Ovh-Consumer: $OVH_CONSUMER_KEY" \
      -H "X-Ovh-Timestamp: $now" \
      -H "X-Ovh-Signature: $sig"
  fi
}

ovh_get()    { ovh_call GET    "$1"; }
ovh_delete() { ovh_call DELETE "$1"; }
