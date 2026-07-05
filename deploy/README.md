# Deploying baasparse

Container image + Helm chart for running the mediation engine **multi-replica** on
Kubernetes, over S3 object storage, with OpenTelemetry. This is the cloud-native
target described in [`docs/technical-spec/16-cloud-native-deployment.md`](../docs/technical-spec/16-cloud-native-deployment.md).

```
deploy/
├── Dockerfile                 # static Go binary on distroless:nonroot
└── helm/baasparse/            # Helm chart
    ├── values.yaml            # all knobs (dev defaults target microk8s)
    └── templates/
        ├── config.yaml        # ConfigMap (non-secret env) + Secret (DB/S3/admin)
        ├── deployment.yaml    # the engine — N replicas, per-pod instance id
        ├── service.yaml       # Service + PodDisruptionBudget + HPA + Ingress
        ├── jobs.yaml          # pre-install migrate + post-install createadmin
        ├── minio.yaml         # bundled S3 (dev only)
        └── otel-collector.yaml# bundled OTLP collector (dev only)
```

## How it runs

- **Multiple replicas, one instance identity each.** Each pod takes its
  `BAASPARSE_INSTANCE_ID` from its own pod name (Downward API). Files are claimed
  through a Postgres lease (`FC_FILE_CLAIM`), so exactly one replica processes any
  given file; a dead pod's lease expires and another takes over. See TS 16 and
  BR-HA-003/004.
- **Object storage substrate.** In-cluster there is no shared RWX volume — input,
  output and done areas are S3 key prefixes. SFTP/filesystem ingestion is still
  supported for non-object-store sources (TS 16 §16.2); this chart wires the S3
  backend.
- **Schema & admin via Jobs.** A pre-install Job applies the schema; a post-install
  Job creates/resets the bootstrap admin. The engine pods run with
  `--auto-migrate=false`.
- **Graceful drain.** On `SIGTERM` a pod stops claiming new files but lets an
  in-flight file finish (bounded); `terminationGracePeriodSeconds` (45s) exceeds
  the drain grace so the record commits before the pod dies (BR-HA-008).
- **Observability.** Every pod serves Prometheus metrics at `/metrics` and exports
  OTLP traces/metrics/logs to the collector, which fans out to Prometheus / Loki /
  Tempo (endpoints you supply).

Liveness (`/healthz`) is DB-independent so a brief database blip doesn't kill pods;
readiness (`/readyz`) pings the database so traffic only reaches pods that can serve.

---

## Quickstart on microk8s

Assumes a fresh microk8s. Run from the repo root.

### 1. Enable addons

```bash
microk8s enable dns hostpath-storage
# optional, for an Ingress instead of port-forward:
microk8s enable ingress
```

### 2. Build the image and import it into microk8s

```bash
docker build -f deploy/Dockerfile -t baasparse:dev .
docker save baasparse:dev | microk8s ctr image import -
```

(No Docker? `microk8s ctr` can import any OCI tar — build with `podman`/`buildah`
and `podman save` instead.)

### 3. Provide a PostgreSQL

The chart expects an **external** database (managed in production). For a dev
cluster, deploy a throwaway one in the same namespace:

```bash
microk8s kubectl create namespace baasparse
microk8s kubectl -n baasparse apply -f - <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata: { name: postgres, labels: { app: postgres } }
spec:
  replicas: 1
  selector: { matchLabels: { app: postgres } }
  template:
    metadata: { labels: { app: postgres } }
    spec:
      containers:
        - name: postgres
          image: postgres:18
          env:
            - { name: POSTGRES_PASSWORD, value: admin }
            - { name: POSTGRES_DB, value: baasparseDB }
          ports: [{ containerPort: 5432 }]
---
apiVersion: v1
kind: Service
metadata: { name: postgres }
spec:
  selector: { app: postgres }
  ports: [{ port: 5432, targetPort: 5432 }]
EOF
microk8s kubectl -n baasparse rollout status deploy/postgres
```

This matches the chart's default `database.url`
(`postgres://postgres:admin@postgres:5432/baasparseDB`), so no override is needed.

### 4. Install the chart

```bash
microk8s helm3 install baasparse deploy/helm/baasparse \
  -n baasparse \
  --set admin.password='ChangeMe#123'
```

The bundled **MinIO** and **OTel Collector** are enabled by default, so this is
fully self-contained. Watch it come up:

```bash
microk8s kubectl -n baasparse rollout status deploy/baasparse-baasparse
microk8s kubectl -n baasparse get pods
```

You should see 3 `baasparse-baasparse-…` pods plus `-minio` and `-otel-collector`,
and the two hook Jobs `Completed`.

### 5. Open the GUI and sign in

```bash
microk8s kubectl -n baasparse port-forward svc/baasparse-baasparse 8080:80
```

Browse <http://localhost:8080/> and sign in as `admin` / the password you set.

### 6. See multi-replica claiming in action

Create a pipeline in the GUI (input/output as S3 prefixes, e.g. `input` → `output`),
upload files, then watch different pods pick up different files:

```bash
microk8s kubectl -n baasparse logs -l app.kubernetes.io/component=server \
  --prefix --tail=20 | grep -Ei 'processed|claim|quarantin'
```

Each file is recorded once (`PF_PROCESSED_FILE`), regardless of which replica won
the claim.

---

## Configuration reference

All knobs live in `helm/baasparse/values.yaml`. The ones you'll touch most:

| Value | Default | Purpose |
|-------|---------|---------|
| `replicaCount` | `3` | Engine replicas (each a distinct claim identity). |
| `image.repository` / `image.tag` | `baasparse` / `dev` | Image to run. |
| `database.url` | dev placeholder | DSN when **not** using an external Secret. |
| `database.existingSecret` / `existingSecretKey` | `""` / `DATABASE_URL` | Read the DSN from an existing Secret instead. |
| `storage.backend` | `s3` | `s3` or `posix`. |
| `storage.s3.*` | MinIO creds | Endpoint/bucket/keys; endpoint defaults to bundled MinIO. |
| `admin.username` / `admin.password` | `admin` / `change-me` | Bootstrap admin (createadmin Job). |
| `otel.endpoint` | bundled collector | OTLP endpoint; empty disables export. |
| `minio.enabled` | `true` | Bundle a dev MinIO (ephemeral). |
| `otelCollector.*` | enabled | Bundle a collector; set downstream Prom/Loki/Tempo endpoints. |
| `ingress.enabled` | `false` | Expose via Ingress (+ optional cert-manager TLS). |
| `autoscaling.enabled` / `podDisruptionBudget.enabled` | `false` / `true` | HPA / PDB. |

Precedence inside a pod: **environment (ConfigMap/Secret) → defaults**. The
`~/.baasparse/.config` file is the local/dev layer only and is absent in-container.

---

## Production

The dev defaults are **not** production-safe (ephemeral MinIO, placeholder DB DSN,
`change-me` admin). For production:

1. **External managed PostgreSQL.** Put the DSN in a Secret and reference it:
   ```bash
   kubectl -n baasparse create secret generic baasparse-db \
     --from-literal=DATABASE_URL='postgres://user:pass@db.internal:5432/baasparse?sslmode=require'
   helm install baasparse deploy/helm/baasparse -n baasparse \
     --set database.existingSecret=baasparse-db
   ```
   (The chart still creates its own Secret for the admin + S3 keys — only the DSN
   comes from the external one.)

2. **External S3.** Disable MinIO and point at your bucket:
   ```
   --set minio.enabled=false \
   --set storage.s3.endpoint=s3.eu-west-1.amazonaws.com \
   --set storage.s3.useSSL=true \
   --set storage.s3.bucket=my-mediation-bucket \
   --set storage.s3.accessKey=… --set storage.s3.secretKey=…
   ```
   (Or better: pre-create the chart Secret's `BAASPARSE_S3_SECRET_KEY` via
   `--set-string` from your secret store / CI rather than a shell history.)

3. **Real admin password:** `--set admin.password="$(strong secret)"`. The password
   reaches the createadmin Job through the Secret + `$(VAR)` env expansion — it is
   never written into a manifest argument or logged.

4. **Telemetry downstream.** Either disable the bundled collector and set
   `otel.endpoint` to your platform's OTLP gateway, or keep it and wire its
   exporters:
   ```
   --set otelCollector.prometheusRemoteWrite=http://prometheus:9090/api/v1/write \
   --set otelCollector.lokiEndpoint=http://loki:3100/otlp \
   --set otelCollector.tempoEndpoint=tempo:4317
   ```
   Alerting is delegated to Alertmanager (define rules against the exported
   metrics), per the TS 16 decision.

5. **Ingress + TLS:** `--set ingress.enabled=true --set ingress.host=baasparse.example.com --set ingress.tls=true --set ingress.clusterIssuer=letsencrypt-prod`.

6. **Scale:** raise `replicaCount`, or `--set autoscaling.enabled=true` (HPA on CPU).
   The PodDisruptionBudget (`minAvailable: 1`) keeps at least one replica during
   voluntary disruptions.

---

## Operations

- **Upgrade / config change:** `helm upgrade baasparse deploy/helm/baasparse -n baasparse --reuse-values …`.
  The Deployment re-rolls automatically when the ConfigMap/Secret changes
  (`checksum/config` annotation). The migrate Job re-runs on upgrade (schema is
  idempotent).
- **Scale:** `kubectl -n baasparse scale deploy/baasparse-baasparse --replicas=6`
  (or via HPA). New replicas immediately participate in claiming.
- **Drain / restart:** a rolling restart is safe — a terminating pod finishes its
  in-flight file (bounded by the grace period) before exiting.
- **Startup reconciliation:** on boot, a single replica (advisory-lock guarded)
  reconciles the DB against completion markers in the `done` area, recovering any
  finished files the database is missing (e.g. after a restore behind storage).

## Troubleshooting

| Symptom | Check |
|---------|-------|
| Pods `CrashLoopBackOff` at start | `kubectl logs` — usually the DB DSN (`database.url`/Secret) is wrong or Postgres isn't reachable. Readiness pings the DB. |
| `-migrate` Job failing | Same DB DSN; view `kubectl logs job/baasparse-baasparse-migrate`. |
| `-createadmin` Job failing | Admin values must be non-empty; the Job passes all four fields as flags so it never blocks on a prompt. |
| GUI reachable but no processing | The pipeline needs a published source and an enabled state; check `component=server` pod logs for `list input` / `claim` lines. |
| Metrics missing in Prometheus | Confirm the `prometheus.io/scrape` pod annotations are honoured by your scrape config; each pod serves `/metrics` on the app port. |

> The Docker/Helm assets are authored to spec but validated by you on the cluster
> (no in-repo cluster runtime). The Go binary, schema, and multi-replica claim
> logic are exercised by the test suite and live Postgres runs.
