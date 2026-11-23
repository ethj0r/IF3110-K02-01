#!/bin/sh
set -eu

TraceID=$(openssl rand -hex 16)
SpanID=$(openssl rand -hex 8)
Seconds=$(date +%s)
StartTime="${Seconds}000000000"
EndTime="${Seconds}005000000"

send_otlp() {
    curl -fsS --max-time 5 -H 'Content-Type: application/json' \
        --data-binary @- "http://127.0.0.1:4318/v1/$1" > /dev/null
}

send_otlp traces <<EOF
{"resourceSpans":[{
  "resource":{"attributes":[{"key":"service.name","value":{"stringValue":"wbd-telemetry-demo"}}]},
  "scopeSpans":[{"spans":[{
    "traceId":"$TraceID","spanId":"$SpanID","name":"observability.demo","kind":1,
    "startTimeUnixNano":"$StartTime","endTimeUnixNano":"$EndTime"
  }]}]
}]}
EOF

send_otlp logs <<EOF
{"resourceLogs":[{
  "resource":{"attributes":[{"key":"service.name","value":{"stringValue":"wbd-telemetry-demo"}}]},
  "scopeLogs":[{"logRecords":[{
    "traceId":"$TraceID","spanId":"$SpanID","timeUnixNano":"$EndTime","severityNumber":9,
    "body":{"stringValue":"WBD observability demo log"}
  }]}]
}]}
EOF

printf 'Trace ID %s\n' "$TraceID"
printf 'Loki query {service_name="wbd-telemetry-demo"} | trace_id = "%s"\n' "$TraceID"
