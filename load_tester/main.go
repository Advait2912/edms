// main.go — High-concurrency load testing harness for EDMS.
//
// Spawns multiple worker goroutines to hit various EDMS API endpoints,
// tracks latency percentiles (p50, p90, p95, p99), peak memory consumption,
// writes CSV reports, and serves Prometheus-compatible metrics on /metrics.

package main

import (
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type EndpointSpec struct {
	Method string
	Path   string
	Body   []byte
}

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

type Metrics struct {
	totalRequests  int64
	successCount   int64
	failedCount    int64
	status2xx      int64
	status4xx      int64
	status5xx      int64
	peakAllocBytes uint64
	peakSysBytes   uint64
	latencies      []time.Duration
	latenciesMutex sync.Mutex
}

func (m *Metrics) record(duration time.Duration, statusCode int, err error) {
	atomic.AddInt64(&m.totalRequests, 1)
	if err != nil || statusCode >= 500 {
		atomic.AddInt64(&m.failedCount, 1)
	} else {
		atomic.AddInt64(&m.successCount, 1)
	}

	if statusCode >= 200 && statusCode < 300 {
		atomic.AddInt64(&m.status2xx, 1)
	} else if statusCode >= 400 && statusCode < 500 {
		atomic.AddInt64(&m.status4xx, 1)
	} else if statusCode >= 500 {
		atomic.AddInt64(&m.status5xx, 1)
	}

	m.latenciesMutex.Lock()
	m.latencies = append(m.latencies, duration)
	m.latenciesMutex.Unlock()
}

func (m *Metrics) sampleMemory() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	m.latenciesMutex.Lock()
	if ms.Alloc > m.peakAllocBytes {
		m.peakAllocBytes = ms.Alloc
	}
	if ms.Sys > m.peakSysBytes {
		m.peakSysBytes = ms.Sys
	}
	m.latenciesMutex.Unlock()
}

func startPrometheusServer(addr string, m *Metrics) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		total := atomic.LoadInt64(&m.totalRequests)
		success := atomic.LoadInt64(&m.successCount)
		failed := atomic.LoadInt64(&m.failedCount)
		s2xx := atomic.LoadInt64(&m.status2xx)
		s4xx := atomic.LoadInt64(&m.status4xx)
		s5xx := atomic.LoadInt64(&m.status5xx)

		m.latenciesMutex.Lock()
		peakAlloc := m.peakAllocBytes
		peakSys := m.peakSysBytes
		m.latenciesMutex.Unlock()

		fmt.Fprintf(w, "# HELP edms_load_requests_total Total requests processed\n")
		fmt.Fprintf(w, "# TYPE edms_load_requests_total counter\n")
		fmt.Fprintf(w, "edms_load_requests_total %d\n\n", total)

		fmt.Fprintf(w, "# HELP edms_load_requests_success Total successful requests\n")
		fmt.Fprintf(w, "# TYPE edms_load_requests_success counter\n")
		fmt.Fprintf(w, "edms_load_requests_success %d\n\n", success)

		fmt.Fprintf(w, "# HELP edms_load_requests_failed Total failed requests\n")
		fmt.Fprintf(w, "# TYPE edms_load_requests_failed counter\n")
		fmt.Fprintf(w, "edms_load_requests_failed %d\n\n", failed)

		fmt.Fprintf(w, "# HELP edms_load_status_codes Responses grouped by status code\n")
		fmt.Fprintf(w, "# TYPE edms_load_status_codes counter\n")
		fmt.Fprintf(w, "edms_load_status_codes{code=\"2xx\"} %d\n", s2xx)
		fmt.Fprintf(w, "edms_load_status_codes{code=\"4xx\"} %d\n", s4xx)
		fmt.Fprintf(w, "edms_load_status_codes{code=\"5xx\"} %d\n\n", s5xx)

		fmt.Fprintf(w, "# HELP edms_load_peak_alloc_bytes Peak heap memory allocated in bytes\n")
		fmt.Fprintf(w, "# TYPE edms_load_peak_alloc_bytes gauge\n")
		fmt.Fprintf(w, "edms_load_peak_alloc_bytes %d\n\n", peakAlloc)

		fmt.Fprintf(w, "# HELP edms_load_sys_bytes Peak system memory obtained in bytes\n")
		fmt.Fprintf(w, "# TYPE edms_load_sys_bytes gauge\n")
		fmt.Fprintf(w, "edms_load_sys_bytes %d\n", peakSys)
	})

	go func() {
		_ = http.ListenAndServe(addr, mux)
	}()
}

func worker(target string, reqChan <-chan EndpointSpec, wg *sync.WaitGroup, client *http.Client, m *Metrics) {
	defer wg.Done()

	for spec := range reqChan {
		url := target + spec.Path
		var bodyReader io.Reader
		if len(spec.Body) > 0 {
			bodyReader = bytes.NewReader(spec.Body)
		}

		req, err := http.NewRequest(spec.Method, url, bodyReader)
		if err != nil {
			m.record(0, 0, err)
			continue
		}
		if len(spec.Body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}

		start := time.Now()
		resp, err := client.Do(req)
		elapsed := time.Since(start)

		if err != nil {
			m.record(elapsed, 0, err)
			continue
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		m.record(elapsed, resp.StatusCode, nil)
	}
}

func main() {
	target := flag.String("target", "http://localhost:3000", "Base URL of the EDMS backend")
	concurrency := flag.Int("concurrency", 20, "Number of concurrent worker goroutines")
	totalReqs := flag.Int("requests", 5000, "Total number of requests to execute")
	durationFlag := flag.Duration("duration", 0, "Max duration to run benchmark (e.g. 30s, 1m)")
	metricsAddr := flag.String("metrics-port", ":9100", "Address to expose Prometheus metrics")
	outputCSV := flag.String("output-csv", "benchmark_report.csv", "Path for the output CSV summary report")
	flag.Parse()

	fmt.Println("================================================================")
	fmt.Println("           EDMS HIGH-CONCURRENCY LOAD TEST HARNESS              ")
	fmt.Println("================================================================")
	fmt.Printf(" Target Base URL : %s\n", *target)
	fmt.Printf(" Concurrency     : %d goroutines\n", *concurrency)
	fmt.Printf(" Total Requests  : %d\n", *totalReqs)
	if *durationFlag > 0 {
		fmt.Printf(" Max Duration    : %v\n", *durationFlag)
	}
	fmt.Printf(" Prometheus Port : %s\n", *metricsAddr)
	fmt.Printf(" Output Report   : %s\n", *outputCSV)
	fmt.Println("----------------------------------------------------------------")

	var m Metrics
	m.sampleMemory()
	startPrometheusServer(*metricsAddr, &m)

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        *concurrency * 2,
			MaxIdleConnsPerHost: *concurrency * 2,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	reqChan := make(chan EndpointSpec, *concurrency*4)
	var wg sync.WaitGroup

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go worker(*target, reqChan, &wg, client, &m)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	startTime := time.Now()
	var stopFlag int32

	// Feeder goroutine
	go func() {
		deadline := time.Time{}
		if *durationFlag > 0 {
			deadline = startTime.Add(*durationFlag)
		}

		for i := 0; i < *totalReqs; i++ {
			if atomic.LoadInt32(&stopFlag) == 1 {
				break
			}
			if !deadline.IsZero() && time.Now().After(deadline) {
				break
			}
			spec := endpoints[rand.Intn(len(endpoints))]
			reqChan <- spec
		}
		close(reqChan)
	}()

	// Periodic progress & memory ticker
	ticker := time.NewTicker(2 * time.Second)
	go func() {
		for range ticker.C {
			if atomic.LoadInt32(&stopFlag) == 1 {
				return
			}
			m.sampleMemory()
			reqs := atomic.LoadInt64(&m.totalRequests)
			elapsed := time.Since(startTime).Seconds()
			rps := float64(reqs) / elapsed

			m.latenciesMutex.Lock()
			memMB := float64(m.peakAllocBytes) / (1024 * 1024)
			m.latenciesMutex.Unlock()

			fmt.Printf("  [Progress] %d requests | Throughput: %.1f req/s | Peak Mem: %.2f MB\n", reqs, rps, memMB)
		}
	}()

	// Wait for completion or interrupt
	select {
	case <-sigChan:
		fmt.Println("\n[!] Benchmark interrupted by user. Finalizing report...")
		atomic.StoreInt32(&stopFlag, 1)
	case <-func() chan struct{} {
		ch := make(chan struct{})
		go func() {
			wg.Wait()
			close(ch)
		}()
		return ch
	}():
	}

	ticker.Stop()
	m.sampleMemory()
	totalElapsed := time.Since(startTime)

	// Calculate statistics
	m.latenciesMutex.Lock()
	lats := make([]time.Duration, len(m.latencies))
	copy(lats, m.latencies)
	peakAlloc := m.peakAllocBytes
	peakSys := m.peakSysBytes
	m.latenciesMutex.Unlock()

	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })

	var sum time.Duration
	for _, l := range lats {
		sum += l
	}

	count := len(lats)
	var avg, min, max, p50, p90, p95, p99 time.Duration
	if count > 0 {
		avg = sum / time.Duration(count)
		min = lats[0]
		max = lats[count-1]
		p50 = lats[int(float64(count)*0.50)]
		p90 = lats[int(float64(count)*0.90)]
		p95 = lats[int(float64(count)*0.95)]
		p99 = lats[int(float64(count)*0.99)]
	}

	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)

	rps := float64(m.totalRequests) / totalElapsed.Seconds()
	peakAllocMB := float64(peakAlloc) / (1024 * 1024)
	peakSysMB := float64(peakSys) / (1024 * 1024)
	totalAllocMB := float64(finalMem.TotalAlloc) / (1024 * 1024)

	fmt.Println("\n================================================================")
	fmt.Println("                     BENCHMARK RESULTS                          ")
	fmt.Println("================================================================")
	fmt.Printf(" Total Requests Executed : %d\n", m.totalRequests)
	fmt.Printf(" Total Elapsed Time      : %v\n", totalElapsed)
	fmt.Printf(" Average Throughput      : %.2f requests/second\n", rps)
	fmt.Printf(" Successful (2xx)        : %d\n", m.status2xx)
	fmt.Printf(" Client Errors (4xx)     : %d\n", m.status4xx)
	fmt.Printf(" Server Errors (5xx)     : %d\n", m.status5xx)
	fmt.Printf(" Network/Conn Errors     : %d\n", m.failedCount)
	fmt.Println("----------------------------------------------------------------")
	fmt.Printf(" Min Latency             : %v\n", min)
	fmt.Printf(" Average Latency         : %v\n", avg)
	fmt.Printf(" Median (p50)            : %v\n", p50)
	fmt.Printf(" 90th Percentile (p90)   : %v\n", p90)
	fmt.Printf(" 95th Percentile (p95)   : %v\n", p95)
	fmt.Printf(" 99th Percentile (p99)   : %v\n", p99)
	fmt.Printf(" Max Latency             : %v\n", max)
	fmt.Println("----------------------------------------------------------------")
	fmt.Printf(" Max Heap Memory (Peak)  : %.2f MB\n", peakAllocMB)
	fmt.Printf(" Peak System Memory (Sys): %.2f MB\n", peakSysMB)
	fmt.Printf(" Cumulative Allocations  : %.2f MB\n", totalAllocMB)
	fmt.Printf(" Garbage Collections (GC): %d\n", finalMem.NumGC)
	fmt.Println("================================================================")

	// Save CSV Report
	file, err := os.Create(*outputCSV)
	if err != nil {
		fmt.Printf("[-] Failed to create CSV report: %v\n", err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	_ = writer.Write([]string{"metric", "value"})
	_ = writer.Write([]string{"target", *target})
	_ = writer.Write([]string{"concurrency", fmt.Sprintf("%d", *concurrency)})
	_ = writer.Write([]string{"total_requests", fmt.Sprintf("%d", m.totalRequests)})
	_ = writer.Write([]string{"elapsed_seconds", fmt.Sprintf("%.3f", totalElapsed.Seconds())})
	_ = writer.Write([]string{"throughput_rps", fmt.Sprintf("%.2f", rps)})
	_ = writer.Write([]string{"status_2xx", fmt.Sprintf("%d", m.status2xx)})
	_ = writer.Write([]string{"status_4xx", fmt.Sprintf("%d", m.status4xx)})
	_ = writer.Write([]string{"status_5xx", fmt.Sprintf("%d", m.status5xx)})
	_ = writer.Write([]string{"latency_min_ms", fmt.Sprintf("%.3f", float64(min.Microseconds())/1000.0)})
	_ = writer.Write([]string{"latency_avg_ms", fmt.Sprintf("%.3f", float64(avg.Microseconds())/1000.0)})
	_ = writer.Write([]string{"latency_p50_ms", fmt.Sprintf("%.3f", float64(p50.Microseconds())/1000.0)})
	_ = writer.Write([]string{"latency_p90_ms", fmt.Sprintf("%.3f", float64(p90.Microseconds())/1000.0)})
	_ = writer.Write([]string{"latency_p95_ms", fmt.Sprintf("%.3f", float64(p95.Microseconds())/1000.0)})
	_ = writer.Write([]string{"latency_p99_ms", fmt.Sprintf("%.3f", float64(p99.Microseconds())/1000.0)})
	_ = writer.Write([]string{"latency_max_ms", fmt.Sprintf("%.3f", float64(max.Microseconds())/1000.0)})
	_ = writer.Write([]string{"max_memory_alloc_mb", fmt.Sprintf("%.2f", peakAllocMB)})
	_ = writer.Write([]string{"peak_sys_memory_mb", fmt.Sprintf("%.2f", peakSysMB)})
	_ = writer.Write([]string{"cumulative_alloc_mb", fmt.Sprintf("%.2f", totalAllocMB)})
	_ = writer.Write([]string{"num_gc", fmt.Sprintf("%d", finalMem.NumGC)})

	fmt.Printf("[+] Benchmark report successfully written to: %s\n", *outputCSV)
}
