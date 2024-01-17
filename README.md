# store-forward-otel

A lightweight Go agent that sits between your application and an OTLP backend. When the backend is unreachable, spans are persisted to disk and automatically replayed on reconnect — zero data loss during network partitions or backend outages.

Built for **GDC edge environments** and any deployment where OTLP backend connectivity is intermittent.

---

## How it works

```
Application (SDK)
      │  OTLP gRPC
      ▼
┌─────────────────────────┐
│  store-forward-otel     │
│  agent (:4317)          │
│                         │
│  ┌──────────────────┐   │
│  │    Forwarder     │   │
│  │                  │   │
│  │  backend up?     │   │
│  │  ├── yes ──────────────▶  Upstream OTLP backend
│  │  └── no  ──────┐  │   │  (Jaeger / GCP Trace / etc.)
│  └────────────────┼──┘   │
│                   ▼      │
│  ┌──────────────────┐    │
│  │   DiskBuffer     │    │
│  │  /var/saf/buffer │    │
│  │  *.span files    │    │
│  └──────┬───────────┘    │
│         │  retry loop    │
│         └──────────────────▶  Upstream (on reconnect)
└─────────────────────────┘
```

---

## Features

- **Zero-loss buffering** — spans written to disk when backend is unreachable
- **Ordered replay** — buffered batches flushed oldest-first on reconnect
- **Capacity cap** — configurable max disk usage; rejects new spans if full rather than silently dropping
- **Transparent proxy** — drop-in between any OTel SDK and existing OTLP backend; no SDK changes needed
- **Distroless image** — minimal attack surface, non-root by default
- **Graceful shutdown** — in-flight gRPC calls completed before exit

---

## Quick Start

### Run locally

```bash
make run
```

Or with custom flags:

```bash
go run ./cmd/agent \
  --listen=:4317 \
  --endpoint=jaeger:14317 \
  --buffer-dir=/tmp/saf-buffer \
  --retry-interval=30s
```

### Docker

```bash
make docker-build
docker run -p 4317:4317 \
  -v /var/saf:/var/saf \
  ghcr.io/ravichandra-eluri/store-forward-otel:latest \
  --endpoint=jaeger:14317
```

### Kubernetes (sidecar)

```yaml
containers:
  - name: saf-agent
    image: ghcr.io/ravichandra-eluri/store-forward-otel:latest
    args:
      - --listen=:4317
      - --endpoint=jaeger-collector.observability.svc.cluster.local:14317
      - --buffer-dir=/var/saf/buffer
      - --max-buffer-mb=512
      - --retry-interval=30s
    ports:
      - containerPort: 4317
    volumeMounts:
      - name: saf-buffer
        mountPath: /var/saf/buffer
volumes:
  - name: saf-buffer
    emptyDir:
      sizeLimit: 512Mi
```

---

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `--listen` | `:4317` | OTLP gRPC listen address |
| `--endpoint` | `localhost:14317` | Upstream OTLP backend endpoint |
| `--buffer-dir` | `/var/saf/buffer` | Directory to store buffered spans |
| `--max-buffer-mb` | `512` | Max disk buffer size in MB |
| `--retry-interval` | `30s` | Interval between flush retry attempts |
| `--flush-timeout` | `10s` | Timeout per individual flush attempt |

---

## Project Structure

```
store-forward-otel/
├── buffer/
│   └── buffer.go        # DiskBuffer: write, drain, delete span batches
├── forwarder/
│   └── forwarder.go     # TraceServiceServer: forward or buffer, flush loop
├── cmd/
│   └── agent/
│       └── main.go      # CLI entrypoint
├── config/
│   └── sample.yaml      # Example configuration
├── Dockerfile
├── Makefile
└── go.mod
```

---

## Roadmap

- [ ] Metrics endpoint (`/metrics`) exposing buffer depth and flush success rate
- [ ] Configurable TLS for both receiver and upstream connection
- [ ] Metrics and logs pipeline support (currently traces only)
- [ ] Kubernetes operator integration via `otel-k8s-controller`

---

## Author

**Ravi Chandra Eluri** — Sr. Golang Engineer · OpenTelemetry · Kubernetes  
[GitHub](https://github.com/ravichandra-eluri)
