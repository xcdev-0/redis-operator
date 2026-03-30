#!/usr/bin/env bash
set -euo pipefail

# Usage:
#   ./sample-files/create-redis-tls-secret.sh [namespace] [secret_name] [common_name]
#
# Example:
#   ./sample-files/create-redis-tls-secret.sh default redis-tls-secret redis-cluster

NAMESPACE="${1:-default}"
SECRET_NAME="${2:-redis-tls-secret}"
COMMON_NAME="${3:-redis-cluster}"
VALID_DAYS="${VALID_DAYS:-365}"

WORK_DIR="$(mktemp -d)"
CERT_PATH="${WORK_DIR}/tls.crt"
KEY_PATH="${WORK_DIR}/tls.key"

cleanup() {
  rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

echo "[1/3] Generating self-signed TLS cert..."
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days "${VALID_DAYS}" \
  -keyout "${KEY_PATH}" \
  -out "${CERT_PATH}" \
  -subj "/CN=${COMMON_NAME}"

echo "[2/3] Creating/updating secret ${NAMESPACE}/${SECRET_NAME}..."
kubectl create secret generic "${SECRET_NAME}" -n "${NAMESPACE}" \
  --from-file=ca.crt="${CERT_PATH}" \
  --from-file=tls.crt="${CERT_PATH}" \
  --from-file=tls.key="${KEY_PATH}" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "[3/3] Done."
echo "Secret: ${NAMESPACE}/${SECRET_NAME}"
echo "Keys: ca.crt, tls.crt, tls.key"
