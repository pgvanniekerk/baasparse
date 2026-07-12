# baasparse — observability on microk8s

Metrics + logs + dashboards for baasparse, using the microk8s **observability**
addon (kube-prometheus-stack: Prometheus + Grafana + Alertmanager, plus Loki +
Promtail for logs). The app is already OpenTelemetry-instrumented and exposes a
Prometheus `/metrics` endpoint on each pod (port 8080), and logs JSON to stdout.

## One-time setup

1. **Enable the addon** (needs sudo — run in a REAL terminal, not Claude Code's
   `!` prefix, which is non-interactive and can't answer the sudo prompt):
   ```bash
   microk8s enable observability
   ```
2. **Let Prometheus scrape baasparse** — a PodMonitor (its `release` label must
   match the stack's `podMonitorSelector`, `release: kube-prom-stack`):
   ```bash
   microk8s kubectl apply -f deploy/observability/baasparse-podmonitor.yaml
   ```
3. **Import the dashboard** (port-forward Grafana, then run the importer):
   ```bash
   microk8s kubectl -n observability port-forward svc/kube-prom-stack-grafana 19093:80 &
   python3 deploy/observability/import-dashboard.py
   ```

## Access

```bash
# Grafana  (login: admin / prom-operator)
microk8s kubectl -n observability port-forward svc/kube-prom-stack-grafana 3000:80
#   → http://localhost:3000  → Dashboards → "baasparse — pipeline overview"
#   → Explore → Loki → {namespace="baasparse"}   (app logs)

# Prometheus (raw queries / target health)
microk8s kubectl -n observability port-forward svc/kube-prom-stack-kube-prome-prometheus 9090:9090
#   → http://localhost:9090  → Status → Targets  (baasparse pods should be UP)
```

## What you get

- **Dashboard** `baasparse-overview`: files processed / quarantined / records-in
  stat tiles, a success ratio, processing-time p50/p95 per pipeline, throughput,
  records in/out/suspended, and quarantines-by-reason.
- **Logs** in Loki automatically (Promtail scrapes every pod's stdout) — no app
  change. Each line carries `trace_id` and `correlationID`.
- See `QUERIES.md` for the raw PromQL / LogQL behind the panels.

## Notes

- This addon build ships **no Tempo**, so distributed traces aren't collected here
  (the app will export them via OTLP if you add a Tempo endpoint and point
  `otelCollector.tempoEndpoint` at it in the chart).
- Metrics/logs live on the addon's hostpath PVCs. To remove everything:
  `microk8s disable observability`.
