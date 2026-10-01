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
- **Canonical EID Formatting**: Generates deterministic EIDs conforming to `E[0-9]{4}-[A-Z]{3}` with base-26 rollover (e.g., `E0001-AAA`, `E9999-AAA`, `E0000-AAB`).
- **Relational Schema Population**: Creates standard EDMS SQLite tables (`endpoints`, `bookmarks`, `tags`, `history`, `collections`) in WAL mode with normalized composite keys.
- **High-Throughput Batch Commits**: Commits records in batches of 25,000 to maximize disk insertion speed (~29,000 rows/second).
- **Physical Payload Files (Optional)**: Can generate physical `globalEQPData/<EID>/` directories with mock `request`, `response`, and `headers` JSON files.
- **Peak Memory Telemetry**: Utilizes Python `tracemalloc` to report memory high-water mark during generation.

### Prerequisites
- Python 3.8+ (uses standard library modules only: `sqlite3`, `json`, `argparse`, `tracemalloc`, `random`, `time`). No external `pip` packages required.

---

### How to Run

```bash
# Basic run: generates default 5,000 endpoints in ./benchmarks/data/edms_synthetic.db
python synthetic_data_gen.py

# Custom scale: generate 10,000 endpoints with mock physical payload files
python synthetic_data_gen.py --count 10000 --with-qp --output-dir ./data --db-name edms.db

# Heavy stress test: generate 100,000 endpoints (database only)
python synthetic_data_gen.py --count 100000 --output-dir ./data --db-name edms_100k.db
```

*(On Linux / macOS / WSL, you can use `python3 synthetic_data_gen.py`)*

---

### Command-Line Arguments & How to Change Them

| Argument | Default | What It Controls | Example of Changing It |
| :--- | :--- | :--- | :--- |
| `--count` | `5000` | Total number of endpoints created. | `--count 20000` (Generates 20,000 endpoints) |
| `--output-dir` | `./benchmarks/data` | Where the database and mock folders will be written. | `--output-dir "C:/edms_data"` or `--output-dir /tmp/data` |
| `--db-name` | `edms_synthetic.db` | The filename of the SQLite database. | `--db-name production_mock.db` |
| `--with-qp` | *Disabled* | When included, generates physical `globalEQPData/<EID>/` JSON mock files (`request`, `response`, `headers`). Omit this flag for fast, database-only benchmarks. | Add `--with-qp` to command |

---

### How to Customize the Generated Data (In Code)

Open `synthetic_data_gen.py` to customize the dataset characteristics:

- **Change HTTP Methods**: Modify the `METHODS` list (line 20):
  ```python
  METHODS = ["GET", "POST", "PUT", "DELETE", "PATCH"]
  ```
- **Change Simulated Microservices / Paths**: Modify `SAMPLE_SERVICES` (line 21):
  ```python
  SAMPLE_SERVICES = ["users", "orders", "auth", "inventory", "payments", "analytics"]
  ```
- **Change Default Tags & Collections**: Edit `SAMPLE_TAGS` (line 22) and `SAMPLE_COLLECTIONS` (line 23).
- **Change Batch Commit Size**: In `generate_data()`, change the batch size modulo `i % 25000 == 0` (line 164) if you want larger or smaller commit transactions.

---

## 2. High-Concurrency Load Tester (`load_tester/`)

A high-throughput HTTP benchmarking harness written in Go. Evaluates server performance under concurrent worker loads while monitoring latency distribution and Go runtime memory allocations.

### Key Capabilities
- **Configurable Goroutine Pool**: Distributes load across a configurable number of concurrent worker threads.
- **Representative Traffic Mix**: Rotates requests across standard EDMS endpoints:
  - Read views: `GET /test-view`, `GET /list-view`, `GET /dashboard/snapshot`, `GET /dashboard/static`
  - Metadata: `GET /collections/list`, `GET /tags/popular`, `GET /purge_audit_report`
  - Mutations/IPC: `POST /view/refresh`, `POST /dataview/core-api/merge`
- **Latency Distribution Percentiles**: Computes Min, Average, Median (p50), 90th percentile (p90), 95th percentile (p95), 99th percentile (p99), and Max latency.
- **HTTP Status Breakdown**: Tracks exact counts for `2xx` (success), `4xx` (client errors), and `5xx` (server errors).
- **Go Runtime Memory Telemetry**: Samples `runtime.MemStats` to report Peak Heap Allocation (`Alloc`), System Memory (`Sys`), Cumulative Allocations (`TotalAlloc`), and GC cycles (`NumGC`).
- **CSV Summary Reports**: Automatically writes test results to a CSV file.
- **Prometheus Metrics**: Exposes live OpenMetrics / Prometheus metrics on `:9100/metrics`.

### Prerequisites
- Go 1.20+ OR Docker

---

### How to Run

#### Option A: Running with Go Directly

Navigate into the `load_tester` directory first:
```bash
cd load_tester
```

```bash
# 1. Default test: 20 concurrent workers, 5,000 total requests against http://localhost:3000
go run main.go

# 2. Target a custom server host or port (e.g. port 3911 or 8080)
go run main.go -target "http://localhost:3911"

# 3. High-load stress test: 50 concurrent workers, 20,000 requests, custom report name
go run main.go -target "http://localhost:3000" -concurrency 50 -requests 20000 -output-csv stress_test_report.csv

# 4. Time-bounded test: run continuously for exactly 2 minutes (ignores request count ceiling)
go run main.go -target "http://localhost:3000" -concurrency 30 -duration 2m
```

#### Option B: Building a Standalone Binary (Faster Execution)

If you don't want to recompile on every run:
```bash
cd load_tester
go build -o edms-load-tester main.go

# Run the binary directly:
./edms-load-tester -target "http://localhost:3000" -concurrency 25 -requests 10000
```
*(On Windows PowerShell, run `.\edms-load-tester.exe`)*

#### Option C: Running with Docker

```bash
# Build the Docker image
docker build -t edms-load-tester load_tester/

# Run against a host service (Linux/WSL)
docker run --rm --net=host edms-load-tester -target "http://127.0.0.1:3000" -concurrency 20 -requests 5000

# Run on Windows / macOS Docker Desktop (targeting host.docker.internal)
docker run --rm -p 9100:9100 edms-load-tester -target "http://host.docker.internal:3000" -concurrency 30 -requests 10000
```

---

### Command-Line Flags & How to Change Them

| Flag | Default | What It Controls | Example of Changing It |
| :--- | :--- | :--- | :--- |
| `-target` | `http://localhost:3000` | Base URL of the target EDMS server. | `-target "http://192.168.1.50:8080"` |
| `-concurrency` | `20` | Number of simultaneous worker goroutines sending requests. | `-concurrency 50` (Higher value tests concurrent throughput and socket connection limits) |
| `-requests` | `5000` | Total number of HTTP requests to execute before stopping. | `-requests 25000` |
| `-duration` | `0` (disabled) | Max time limit for the test (e.g. `30s`, `1m`, `5m`). Overrides `-requests` when elapsed. | `-duration 3m` (Runs for 3 minutes continuously) |
| `-metrics-port`| `:9100` | Port where the Prometheus `/metrics` endpoint is hosted. | `-metrics-port ":9191"` |
| `-output-csv` | `benchmark_report.csv` | File path where the CSV report is saved. | `-output-csv "reports/run_10k.csv"` |

---

### How to Customize Target Endpoints & Payloads (In Code)

Open `load_tester/main.go` to adjust which endpoints are hit and how:

- **Add or Remove Endpoints**: Locate `var endpoints` (lines 33–43):
  ```go
  var endpoints = []EndpointSpec{
      {Method: "GET", Path: "/test-view"},
      {Method: "GET", Path: "/list-view"},
      {Method: "GET", Path: "/dashboard/snapshot"},
      {Method: "GET", Path: "/dashboard/static"},
      {Method: "GET", Path: "/collections/list"},
      {Method: "GET", Path: "/tags/popular"},
      {Method: "GET", Path: "/purge_audit_report"},
      {Method: "POST", Path: "/view/refresh", Body: []byte(`{"view_type":"benchmark","count":1}`)},
      {Method: "POST", Path: "/dataview/core-api/merge"},
  }
  ```
  To benchmark a new endpoint (e.g., `GET /health` or `POST /bookmarks/save`), simply add a new line to this array:
  ```go
  {Method: "GET", Path: "/health"},
  {Method: "POST", Path: "/api/custom", Body: []byte(`{"key":"value"}`)},
  ```
- **Change Client Timeouts or Connection Pools**: In `main.go` (lines 201–208), adjust `Timeout` or `IdleConnTimeout`:
  ```go
  client := &http.Client{
      Timeout: 10 * time.Second,
      Transport: &http.Transport{
          MaxIdleConns:        *concurrency * 2,
          MaxIdleConnsPerHost: *concurrency * 2,
          IdleConnTimeout:     90 * time.Second,
      },
  }
  ```

---

## 3. End-to-End Benchmark Workflow

Here is how to run a complete benchmark cycle from scratch:

### Step 1: Generate Test Dataset
```bash
python synthetic_data_gen.py --count 10000 --output-dir ./data --db-name test.db
```

### Step 2: Start the EDMS Webserver
Run your EDMS webserver configured to use the generated `test.db` (listening on port 3000):
```bash
# In your main backend directory:
cargo run --bin edms-webserver
```

### Step 3: Run the Load Tester
In a separate terminal:
```bash
cd load_tester
go run main.go -target "http://localhost:3000" -concurrency 20 -requests 5000 -output-csv 10k_benchmark.csv
```

### Step 4: Analyze Output & Metrics
- **Terminal Progress**: Live updates print every 2 seconds displaying current throughput (`req/s`) and peak memory usage (`MB`).
- **Final Summary**: Prints a detailed latency distribution table (Min, Avg, p50, p90, p95, p99, Max) and HTTP status breakdown.
- **CSV Output**: Check `10k_benchmark.csv` for the exported data.
- **Prometheus**: If running a longer benchmark, monitor `http://localhost:9100/metrics` or attach a Prometheus scraper.
