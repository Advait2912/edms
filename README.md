# EDMS Benchmarking & Telemetry Suite

A standalone benchmarking and load-testing suite designed for EDMS (Endpoint Data Management System). This suite provides automated tools to generate realistic scaled datasets and evaluate backend throughput, latency percentiles, error rates, and memory telemetry under heavy concurrent load.

---

## Suite Components

| Component | Path | Language | Description |
| :--- | :--- | :--- | :--- |
| **Synthetic Data Generator** | `synthetic_data_gen.py` | Python 3 | Generates scaled SQLite databases and mock `globalEQPData/` payload files. |
| **High-Concurrency Load Tester** | `load_tester/` | Go 1.22 | Multi-threaded HTTP load tester with latency percentiles, memory metrics, and Prometheus support. |

---

## 1. Synthetic Data Generator (`synthetic_data_gen.py`)

Generates realistic mock datasets across arbitrary scales (e.g., 1K, 5K, 10K, 50K, 100K endpoints) with strict structural compliance to the EDMS schema.

### Key Capabilities
- **Canonical EID Formatting**: Generates deterministic EIDs conforming to `E[0-9]{4}-[A-Z]{3}` with base-26 rollover (e.g. `E0001-AAA`, `E9999-AAA`, `E0000-AAB`).
- **Relational Schema Population**: Creates standard EDMS SQLite tables (`endpoints`, `bookmarks`, `tags`, `history`, `collections`) in WAL mode with normalized schema.
- **High-Throughput Batch Commits**: Commits records in batches of 25,000 to maximize disk insertion speed (~29,000 rows/second).
- **Physical Payload Files (Optional)**: Can optionally generate physical `globalEQPData/<EID>/` directories with mock `request`, `response`, and `headers` JSON files.
- **Peak Memory Telemetry**: Utilizes Python `tracemalloc` to report memory high-water mark during generation.

### Prerequisites
- Python 3.8+ (uses standard library modules: `sqlite3`, `json`, `argparse`, `tracemalloc`, `random`, `time`). No external `pip` dependencies required.

### Usage

```bash
# Basic run: generate default 5,000 endpoints
python synthetic_data_gen.py

# Generate 10,000 endpoints to a specific directory with physical payload files
python synthetic_data_gen.py --count 10000 --with-qp --output-dir ./data --db-name edms.db

# Large scale test: generate 100,000 endpoints (database only)
python synthetic_data_gen.py --count 100000 --output-dir ./data --db-name edms_100k.db
```

### Command-Line Arguments

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--count` | `5000` | Total number of endpoints to generate (e.g., 1000, 5000, 10000, 50000, 100000). |
| `--output-dir` | `./benchmarks/data` | Output directory path for the SQLite database and storage folders. |
| `--db-name` | `edms_synthetic.db` | Name of the generated SQLite database file. |
| `--with-qp` | *Disabled* | Flag to generate physical `globalEQPData/<EID>/` mock JSON payload files. |

---

## 2. High-Concurrency Load Tester (`load_tester/`)

A high-throughput HTTP benchmarking harness written in Go. Evaluates server performance under concurrent worker loads while monitoring latency distribution and Go runtime memory allocations.

### Key Capabilities
- **Configurable Worker Pool**: Distributes load across a configurable number of concurrent worker goroutines.
- **Representative Traffic Mix**: Rotates requests across standard EDMS endpoints:
  - Read views: `GET /test-view`, `GET /list-view`, `GET /dashboard/snapshot`, `GET /dashboard/static`
  - Metadata: `GET /collections/list`, `GET /tags/popular`, `GET /purge_audit_report`
  - Mutations/IPC: `POST /view/refresh`, `POST /dataview/core-api/merge`
- **Latency Distribution Percentiles**: Computes Min, Average, Median (p50), 90th percentile (p90), 95th percentile (p95), 99th percentile (p99), and Max latency.
- **HTTP Status Code Accounting**: Tracks exact counts for `2xx` (success), `4xx` (client errors), and `5xx` (server errors).
- **Go Runtime Memory Telemetry**: Samples `runtime.MemStats` to report Peak Heap Allocation (`Alloc`), System Memory (`Sys`), Cumulative Allocations (`TotalAlloc`), and GC cycles (`NumGC`).
- **CSV Summary Reports**: Automatically writes test results to a CSV file.
- **Prometheus Metrics**: Exposes live OpenMetrics / Prometheus metrics on `:9100/metrics`.

### Prerequisites
- Go 1.20+ OR Docker

### Running with Go

```bash
cd load_tester

# Run against local backend with default settings (20 workers, 5,000 requests)
go run main.go

# Custom parameters: 50 concurrent workers, 10,000 requests against custom host
go run main.go -target "http://localhost:3000" -concurrency 50 -requests 10000 -output-csv report.csv

# Time-bounded run: run for max 60 seconds
go run main.go -target "http://localhost:3000" -concurrency 25 -duration 60s
```

### Running with Docker

```bash
# Build the load-tester image
docker build -t edms-load-tester load_tester/

# Run against host network (Linux/WSL)
docker run --rm --net=host edms-load-tester -target "http://127.0.0.1:3000" -concurrency 20 -requests 5000

# Run with custom Prometheus port exposed
docker run --rm -p 9100:9100 edms-load-tester -target "http://host.docker.internal:3000" -concurrency 30 -requests 10000
```

### Command-Line Flags

| Flag | Default | Description |
| :--- | :--- | :--- |
| `-target` | `http://localhost:3000` | Base URL of the target EDMS webserver. |
| `-concurrency` | `20` | Number of concurrent worker goroutines. |
| `-requests` | `5000` | Total number of HTTP requests to execute. |
| `-duration` | `0` (disabled) | Max time duration to run (e.g. `30s`, `2m`). If specified, stops when time expires. |
| `-metrics-port` | `:9100` | Address/port to serve Prometheus metrics (`/metrics`). |
| `-output-csv` | `benchmark_report.csv` | File path to write the CSV summary report. |

---

## 3. End-to-End Benchmark Workflow

Follow these steps to conduct a complete benchmark cycle against an EDMS instance:

### Step 1: Generate Test Dataset
```bash
python synthetic_data_gen.py --count 10000 --output-dir ./data --db-name test.db
```

### Step 2: Start the EDMS Webserver
Configure the EDMS webserver to load the generated `test.db` and start the service (e.g., on `http://localhost:3000`).

### Step 3: Run the Load Tester
```bash
cd load_tester
go run main.go -target "http://localhost:3000" -concurrency 20 -requests 5000 -output-csv 10k_benchmark.csv
```

### Step 4: Inspect Metrics & Reports
- **Console Output**: Live progress and final summary tables printed to stdout.
- **CSV Report**: Check `10k_benchmark.csv` for machine-readable performance metrics.
- **Prometheus**: If running prolonged duration tests, scrape `http://localhost:9100/metrics`.
