# baasparse — monitoring queries (Grafana / Prometheus)

The app already exports these metrics from each pod's `/metrics` (OTel Prometheus
exporter). Every series is labelled by `pipeline` (and `pod`/`instance` once
scraped by Prometheus), so you can slice per-pipeline or aggregate.

| Metric | Type | Meaning |
|---|---|---|
| `baasparse_files_processed_total` | counter | files completed (DONE) |
| `baasparse_records_in_total` | counter | records decoded |
| `baasparse_records_out_total` | counter | records distributed |
| `baasparse_records_suspended_total` | counter | records suspended |
| `baasparse_file_processing_seconds` | histogram | per-file processing time (`_bucket`/`_sum`/`_count`) |
| `baasparse_files_quarantined_total` | counter | files quarantined, labelled by `pipeline` + `reason_code` |

## Processing time

```promql
# p95 processing time per pipeline (5m window)
histogram_quantile(0.95, sum by (pipeline, le) (rate(baasparse_file_processing_seconds_bucket[5m])))

# p50 / median
histogram_quantile(0.50, sum by (pipeline, le) (rate(baasparse_file_processing_seconds_bucket[5m])))

# average processing time per pipeline
sum by (pipeline) (rate(baasparse_file_processing_seconds_sum[5m]))
  / sum by (pipeline) (rate(baasparse_file_processing_seconds_count[5m]))
```

## Throughput & success counters

```promql
# files processed per second (throughput), per pipeline
sum by (pipeline) (rate(baasparse_files_processed_total[5m]))

# records in vs out per second (per pipeline)
sum by (pipeline) (rate(baasparse_records_in_total[5m]))
sum by (pipeline) (rate(baasparse_records_out_total[5m]))

# total files processed since start (single stat)
sum(baasparse_files_processed_total)
```

### Records / second (dashboard "Records / second" panels)

```promql
# records parsed (decoded) per second, cluster-wide  — use [1m] for a responsive line
sum(rate(baasparse_records_in_total[1m]))
# records processed (distributed) per second
sum(rate(baasparse_records_out_total[1m]))
# per pipeline
sum by (pipeline) (rate(baasparse_records_in_total[1m]))

# Engine throughput — records per ACTIVE processing-second (window-independent):
# records ingested ÷ time spent processing. Shows the true parse speed (~25k/s in
# the benchmark) regardless of how idle the wall-clock window is.
sum(rate(baasparse_records_in_total[1m]))
  / clamp_min(sum(rate(baasparse_file_processing_seconds_sum[1m])), 1e-9)
```

Note: `records_in_total` jumps by a whole file's record count when the file
completes, so a `rate()` spreads that burst over the window — a continuous feed
reads accurately, but a single small file barely registers on the wall-clock
rate. The **engine-throughput** query above avoids that (it divides records by
processing-time, not wall-clock). The Prometheus scrape interval is **5s**
(`deploy/observability/baasparse-podmonitor.yaml`) so these rates react quickly.

## Errors / suspensions

```promql
# suspended records per second (record-level failures)
sum by (pipeline) (rate(baasparse_records_suspended_total[5m]))

# suspension ratio (suspended / decoded)
sum(rate(baasparse_records_suspended_total[5m])) / sum(rate(baasparse_records_in_total[5m]))

# files quarantined per second, by reason code (whole-file failures)
sum by (pipeline, reason_code) (rate(baasparse_files_quarantined_total[5m]))

# success ratio: DONE vs (DONE + quarantined)
sum(rate(baasparse_files_processed_total[5m]))
  / (sum(rate(baasparse_files_processed_total[5m])) + sum(rate(baasparse_files_quarantined_total[5m])))
```

The full quarantine reason text stays in the logs (`msg="file quarantined"`,
Loki: `{namespace="baasparse"} |= "file quarantined"`) and on the GUI
**Processed files** page; the metric carries just the low-cardinality `reason_code`.

## Logs (Loki, via the addon's log agent)

```logql
{namespace="baasparse"}                                  # all app logs
{namespace="baasparse"} | json | level="ERROR"           # errors only (JSON-parsed)
{namespace="baasparse"} |= "file quarantined"            # quarantine events
{namespace="baasparse"} | json | correlationID="42"      # one file end-to-end
```

Each log line carries `trace_id`, so you can jump log → trace in Tempo.
