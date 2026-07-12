# Accessing baasparse & its observability UIs

All UIs are exposed on **fixed localhost ports** by always-on `systemd --user`
port-forward services (see [Auto port-forwards](#auto-port-forwards) below). They
survive pod restarts (they forward to the Service, not a pod) and come up
automatically when you log in.

## Endpoints

| UI | URL | Login | What it's for |
|---|---|---|---|
| **baasparse GUI** | http://localhost:8080 | `admin` / `Demo#Admin1` | pipelines, datasources, processed files |
| **Grafana** | http://localhost:3000 | `admin` / `prom-operator` | dashboards, logs (Loki), traces (Tempo) |
| **Prometheus** | http://localhost:9090 | — | raw metrics, scrape targets, alert rules |
| **Alertmanager** | http://localhost:9093 | — | alert routing, silences |

**Loki (logs)** and **Tempo (traces)** have no standalone UI — you use them
*inside Grafana* via **Explore** (pick the Loki or Tempo datasource).

## In Grafana

- **Dashboard**: Dashboards → **"baasparse — pipeline overview"** (processing-time
  p50/p95, throughput, success/quarantine counters, records in/out).
- **Logs**: Explore → **Loki** → `{namespace="baasparse"}`
  (e.g. `{namespace="baasparse"} |= "file quarantined"`, or `| json | level="ERROR"`).
- **Traces**: Explore → **Tempo** → Search, service `baasparse` (each file is a
  `ProcessFile` trace). A log line's `trace_id` links straight to its trace.
- Raw queries / cheat-sheet: `deploy/observability/QUERIES.md`.

## Auto port-forwards

Four user-level systemd services keep the tunnels up:

```
baasparse-pf-gui.service          localhost:8080 -> svc/baasparse-baasparse (ns baasparse)
baasparse-pf-grafana.service      localhost:3000 -> svc/kube-prom-stack-grafana (ns observability)
baasparse-pf-prometheus.service   localhost:9090 -> svc/kube-prom-stack-kube-prome-prometheus
baasparse-pf-alertmanager.service localhost:9093 -> svc/kube-prom-stack-kube-prome-alertmanager
```

They're defined in `~/.config/systemd/user/baasparse-pf-*.service`, enabled with
`Restart=always`, and start at login.

### Manage them

```bash
systemctl --user status  baasparse-pf-grafana        # one service
systemctl --user restart baasparse-pf-grafana
systemctl --user stop    'baasparse-pf-*'            # all
journalctl --user -u baasparse-pf-grafana -f         # logs for one
```

### Start at boot (before you log in) — optional, needs sudo once

By default the tunnels start when you log into your desktop session. To have them
run from boot even before login:

```bash
sudo loginctl enable-linger $USER
```

### Re-create on another machine / after a wipe

The unit files are plain text; re-run the block in `deploy/access.md` history, or
recreate them and:

```bash
systemctl --user daemon-reload
systemctl --user enable --now baasparse-pf-gui baasparse-pf-grafana baasparse-pf-prometheus baasparse-pf-alertmanager
```

## Ad-hoc access without the services

If the services aren't installed (e.g. a fresh clone), port-forward manually:

```bash
microk8s kubectl -n baasparse      port-forward svc/baasparse-baasparse 8080:80
microk8s kubectl -n observability  port-forward svc/kube-prom-stack-grafana 3000:80
```
(prefix with `sg microk8s -c '...'` if your shell isn't in the `microk8s` group).
