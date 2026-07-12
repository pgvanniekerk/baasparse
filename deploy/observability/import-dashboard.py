import json, urllib.request, base64, sys

GRAF = "http://localhost:19093"
AUTH = "Basic " + base64.b64encode(b"admin:prom-operator").decode()
RATE = "[1m]"   # rate window for /sec panels (>= 4x the 5s scrape interval)

def req(method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(GRAF + path, data=data, method=method,
                               headers={"Authorization": AUTH, "Content-Type": "application/json"})
    with urllib.request.urlopen(r, timeout=10) as resp:
        return json.loads(resp.read().decode())

ds = req("GET", "/api/datasources")
prom = next((d for d in ds if d.get("type") == "prometheus"), None)
if not prom:
    print("ERROR: no prometheus datasource in Grafana"); sys.exit(1)
UID = prom["uid"]
print("prometheus datasource uid =", UID)

def dsref():
    return {"type": "prometheus", "uid": UID}

def stat(title, expr, x, y, w=6, h=4, unit="short", decimals=None):
    fc = {"defaults": {"unit": unit}, "overrides": []}
    if decimals is not None:
        fc["defaults"]["decimals"] = decimals
    return {"type": "stat", "title": title, "datasource": dsref(),
            "gridPos": {"x": x, "y": y, "w": w, "h": h}, "fieldConfig": fc,
            "options": {"reduceOptions": {"calcs": ["lastNotNull"]}},
            "targets": [{"expr": expr, "datasource": dsref(), "refId": "A"}]}

def ts(title, targets, x, y, w=12, h=8, unit="short"):
    return {"type": "timeseries", "title": title, "datasource": dsref(),
            "gridPos": {"x": x, "y": y, "w": w, "h": h},
            "fieldConfig": {"defaults": {"unit": unit, "custom": {"drawStyle": "line", "fillOpacity": 12}}, "overrides": []},
            "targets": [{"expr": e, "legendFormat": l, "datasource": dsref(), "refId": chr(65+i)} for i,(e,l) in enumerate(targets)]}

thru = ("sum(rate(baasparse_records_in_total%s)) / clamp_min(sum(rate(baasparse_file_processing_seconds_sum%s)), 1e-9)" % (RATE, RATE))

panels = [
    # --- records/sec headline stats ---
    stat("Records parsed / sec", "sum(rate(baasparse_records_in_total%s))" % RATE, 0, 0, unit="short", decimals=0),
    stat("Records processed / sec", "sum(rate(baasparse_records_out_total%s))" % RATE, 6, 0, unit="short", decimals=0),
    stat("Files / sec", "sum(rate(baasparse_files_processed_total%s))" % RATE, 12, 0, unit="short", decimals=2),
    stat("Engine throughput (records / active-sec)", thru, 18, 0, unit="short", decimals=0),

    # --- records/sec timeseries ---
    ts("Records / second — parsed vs processed", [
        ("sum(rate(baasparse_records_in_total%s))" % RATE, "parsed (in)"),
        ("sum(rate(baasparse_records_out_total%s))" % RATE, "processed (out)"),
        ("sum(rate(baasparse_records_suspended_total%s))" % RATE, "suspended"),
    ], 0, 4, w=12),
    ts("Records parsed / second by pipeline", [
        ("sum by (pipeline) (rate(baasparse_records_in_total%s))" % RATE, "{{pipeline}}"),
    ], 12, 4, w=12),

    # --- processing time + files/sec ---
    ts("Processing time per pipeline", [
        ("histogram_quantile(0.95, sum by (pipeline, le) (rate(baasparse_file_processing_seconds_bucket[5m])))", "p95 {{pipeline}}"),
        ("histogram_quantile(0.50, sum by (pipeline, le) (rate(baasparse_file_processing_seconds_bucket[5m])))", "p50 {{pipeline}}"),
    ], 0, 12, w=12, unit="s"),
    ts("Files / second by pipeline", [
        ("sum by (pipeline) (rate(baasparse_files_processed_total%s))" % RATE, "{{pipeline}}"),
    ], 12, 12, w=12),

    # --- totals + quarantine ---
    stat("Files processed (total)", "sum(baasparse_files_processed_total)", 0, 20, w=6),
    stat("Files quarantined (total)", "sum(baasparse_files_quarantined_total)", 6, 20, w=6),
    stat("Success ratio", "sum(baasparse_files_processed_total) / clamp_min(sum(baasparse_files_processed_total) + sum(baasparse_files_quarantined_total), 1)", 12, 20, w=6, unit="percentunit", decimals=3),
    stat("Records parsed (total)", "sum(baasparse_records_in_total)", 18, 20, w=6),
    ts("Quarantines / sec by reason", [
        ("sum by (pipeline, reason_code) (rate(baasparse_files_quarantined_total%s))" % RATE, "{{pipeline}} / {{reason_code}}"),
    ], 0, 24, w=24, h=6),
]

dashboard = {
    "dashboard": {
        "uid": "baasparse-overview",
        "title": "baasparse — pipeline overview",
        "tags": ["baasparse"],
        "timezone": "browser",
        "schemaVersion": 39,
        "refresh": "5s",
        "time": {"from": "now-15m", "to": "now"},
        "panels": panels,
    },
    "overwrite": True,
    "message": "baasparse overview — records/sec added",
}

res = req("POST", "/api/dashboards/db", dashboard)
print("imported:", res.get("status"), "->", GRAF + res.get("url", ""))
