#!/usr/bin/env bash
# baasparse — build, push, and (re)deploy to microk8s, then verify DB-backed readiness.
#
# Reliable + quick local loop:
#   • builds the image with BuildKit module/build caches (only changed pkgs recompile)
#   • pushes to Docker Hub under a UNIQUE tag, so microk8s always pulls the new image
#   • deploys N replicas that reach the host Postgres via the baasparse-db Service
#   • `helm --wait` blocks until pods are Ready, and readiness pings Postgres — so a
#     green run PROVES reliable DB access. DB bootstrap Jobs stay OFF (devops runs
#     `baasparse setupdb` / `createadmin` deliberately).
#
# Prereqs: caller is in the `docker` and `microk8s` groups; host Postgres accepts the
# cluster (see deploy/local/postgres-endpoint.yaml). Override anything via env:
#   REPO TAG NS REPLICAS DB_DSN KUBECTL HELM
set -euo pipefail

REPO="${REPO:-pgvanniekerk/baasparse}"
TAG="${TAG:-$(git rev-parse --short HEAD 2>/dev/null || echo dev).$(date +%s)}"
NS="${NS:-baasparse}"
REPLICAS="${REPLICAS:-2}"
DB_DSN="${DB_DSN:-postgres://postgres:admin@baasparse-db:5432/baasparseDB?sslmode=disable}"
KUBECTL="${KUBECTL:-microk8s kubectl}"
HELM="${HELM:-microk8s helm3}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMG="${REPO}:${TAG}"

echo "==> [1/5] build ${IMG}"
# Classic builder (DOCKER_BUILDKIT=0) works without the buildx plugin; layer caching
# still keeps rebuilds fast. Install docker-buildx to switch to BuildKit if you like.
DOCKER_BUILDKIT="${DOCKER_BUILDKIT:-0}" docker build -f "${ROOT}/deploy/Dockerfile" -t "${IMG}" "${ROOT}"

echo "==> [2/5] push ${IMG}"
docker push "${IMG}"

echo "==> [3/5] ensure host-Postgres Service/Endpoints"
${KUBECTL} apply -f "${ROOT}/deploy/local/postgres-endpoint.yaml"

echo "==> [4/5] deploy ${REPLICAS} replica(s) — DB bootstrap Jobs stay off"
# Local secrets (the AES key that encrypts stored datasource credentials) come from
# a gitignored override — never from the chart defaults. The chart hard-fails if it
# is missing, because a keyless deploy silently gives every pod its OWN key and
# corrupts credentials across replicas. See deploy/local/README.md.
LOCAL_VALUES="${ROOT}/deploy/local/values.local.yaml"
if [ ! -f "${LOCAL_VALUES}" ]; then
  echo "ERROR: ${LOCAL_VALUES} is missing — it holds this cluster's secretKey." >&2
  echo "       Create it (see deploy/local/README.md):" >&2
  echo "         printf 'secretKey: \"%s\"\\n' \"\$(openssl rand -base64 32)\" > ${LOCAL_VALUES}" >&2
  echo "       Do NOT invent a new key if datasource credentials already exist: they" >&2
  echo "       were encrypted with the old one and cannot be recovered." >&2
  exit 1
fi

${HELM} upgrade --install baasparse "${ROOT}/deploy/helm/baasparse" \
  -n "${NS}" --create-namespace \
  -f "${LOCAL_VALUES}" \
  --set replicaCount="${REPLICAS}" \
  --set image.repository="${REPO}" --set image.tag="${TAG}" \
  --set-string database.url="${DB_DSN}" \
  --wait --timeout 240s

echo "==> [5/5] status (Ready == Postgres reachable, since /readyz pings the DB)"
${KUBECTL} -n "${NS}" rollout status deploy/baasparse-baasparse --timeout=120s
${KUBECTL} -n "${NS}" get pods -o wide
echo
echo "OK  ${IMG}  ->  ns/${NS}  (${REPLICAS} replicas)  DB-backed readiness GREEN"
