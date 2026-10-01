#!/usr/bin/env python3
"""
synthetic_data_gen.py — High-performance synthetic dataset generator for EDMS.

Generates realistic, scaled datasets (1K, 5K, 10K, 50K, 100K endpoints) with:
- Canonical EID formatting (E[0-9]{4}-[A-Z]{3})
- SQLite database tables: endpoints, bookmarks, tags, history, collections
- Physical globalEQPData folders with request/response/headers JSON files
"""

import argparse
import json
import os
import random
import sqlite3
import sys
import time
import tracemalloc

METHODS = ["GET", "POST", "PUT", "DELETE", "PATCH"]
SAMPLE_SERVICES = ["users", "orders", "auth", "inventory", "payments", "analytics", "notifications", "search"]
SAMPLE_TAGS = ["production", "v1", "v2", "deprecated", "internal", "public", "auth-required", "cached", "p1", "p2"]
SAMPLE_COLLECTIONS = ["core-api", "admin-panel", "mobile-endpoints", "payment-service", "partner-gateway"]

def index_to_suffix(idx: int) -> str:
    """Converts a 0-indexed integer into a 3-letter uppercase ASCII code (0 -> AAA, 1 -> AAB)."""
    c3 = chr(ord('A') + (idx % 26))
    idx //= 26
    c2 = chr(ord('A') + (idx % 26))
    idx //= 26
    c1 = chr(ord('A') + (idx % 26))
    return f"{c1}{c2}{c3}"

def format_eid(number: int) -> str:
    """Generates canonical EID format: E[0-9]{4}-[A-Z]{3} with base-26 rollover."""
    num_part = (number % 10000)
    suffix_idx = number // 10000
    suffix = index_to_suffix(suffix_idx)
    return f"E{num_part:04d}-{suffix}"

def init_db(db_path: str):
    """Initializes standard EDMS schema in the SQLite database."""
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    cur.executescript("""
        PRAGMA journal_mode = WAL;
        PRAGMA synchronous = NORMAL;

        CREATE TABLE IF NOT EXISTS endpoints (
            endpoint_id TEXT PRIMARY KEY,
            endpoint_str TEXT NOT NULL,
            method TEXT NOT NULL DEFAULT 'GET',
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        );

        CREATE TABLE IF NOT EXISTS bookmarks (
            endpoint_id TEXT NOT NULL,
            folder TEXT NOT NULL,
            notes TEXT,
            timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (endpoint_id, folder)
        );

        CREATE TABLE IF NOT EXISTS tags (
            endpoint_id TEXT NOT NULL,
            tag TEXT NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (endpoint_id, tag)
        );

        CREATE TABLE IF NOT EXISTS history (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            endpoint_id TEXT NOT NULL,
            action TEXT NOT NULL,
            details TEXT,
            timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        );

        CREATE TABLE IF NOT EXISTS collections (
            name TEXT PRIMARY KEY,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        );

        CREATE INDEX IF NOT EXISTS idx_endpoints_str_method ON endpoints(endpoint_str, method);
        CREATE INDEX IF NOT EXISTS idx_bookmarks_folder ON bookmarks(folder);
        CREATE INDEX IF NOT EXISTS idx_tags_tag ON tags(tag);
        CREATE INDEX IF NOT EXISTS idx_history_endpoint ON history(endpoint_id);
        CREATE INDEX IF NOT EXISTS idx_history_timestamp ON history(timestamp);
        CREATE INDEX IF NOT EXISTS idx_history_action ON history(action);
    """)
    conn.commit()
    conn.close()

def generate_data(count: int, output_dir: str, db_name: str, with_qp: bool):
    tracemalloc.start()
    os.makedirs(output_dir, exist_ok=True)
    db_path = os.path.join(output_dir, db_name)
    if os.path.exists(db_path):
        os.remove(db_path)

    print(f"[*] Initializing database at: {db_path}")
    init_db(db_path)

    conn = sqlite3.connect(db_path)
    cur = conn.cursor()

    print(f"[*] Generating {count:,} synthetic endpoints...")
    start_time = time.time()

    # Pre-populate collections
    cur.executemany(
        "INSERT OR IGNORE INTO collections (name) VALUES (?)",
        [(c,) for c in SAMPLE_COLLECTIONS]
    )

    endpoints_batch = []
    bookmarks_batch = []
    tags_batch = []
    history_batch = []

    eqp_root = os.path.join(output_dir, "storage", "globalEQPData") if with_qp else None
    if eqp_root:
        os.makedirs(eqp_root, exist_ok=True)

    for i in range(1, count + 1):
        eid = format_eid(i)
        service = random.choice(SAMPLE_SERVICES)
        method = random.choice(METHODS)
        sub_id = random.randint(1, 5000)
        url = f"/api/v1/{service}/{sub_id}"

        endpoints_batch.append((eid, url, method))

        # 80% of endpoints are bookmarked in 1 or 2 collections
        if random.random() < 0.8:
            cols = random.sample(SAMPLE_COLLECTIONS, k=random.choice([1, 2]))
            for col in cols:
                bookmarks_batch.append((eid, col, f"Generated bookmark for {service}"))

        # Add 1 to 3 random tags
        chosen_tags = random.sample(SAMPLE_TAGS, k=random.randint(1, 3))
        for tag in chosen_tags:
            tags_batch.append((eid, tag))

        # Add history record for 60% of endpoints
        if random.random() < 0.6:
            status = random.choice([200, 200, 201, 204, 400, 404, 500])
            res_time = random.randint(5, 350)
            details = json.dumps({"status": status, "duration_ms": res_time})
            history_batch.append((eid, "EXECUTE", details))

        # Optional physical QP files
        if with_qp and (i <= 5000 or i % 10 == 0):
            eid_dir = os.path.join(eqp_root, eid)
            os.makedirs(eid_dir, exist_ok=True)
            with open(os.path.join(eid_dir, f"{eid}-request-1.json"), "w") as f:
                json.dump({"url": url, "method": method, "headers": {"Content-Type": "application/json"}}, f)
            with open(os.path.join(eid_dir, f"{eid}-response-1.json"), "w") as f:
                json.dump({"status": 200, "data": {"id": sub_id, "service": service}}, f)
            with open(os.path.join(eid_dir, f"{eid}-headers-1.json"), "w") as f:
                json.dump({"date": "Mon, 14 Sep 2026 12:00:00 GMT", "server": "edms/1.0"}, f)

        # Batch insert every 25,000 rows
        if i % 25000 == 0 or i == count:
            cur.executemany("INSERT INTO endpoints (endpoint_id, endpoint_str, method) VALUES (?, ?, ?)", endpoints_batch)
            cur.executemany("INSERT OR IGNORE INTO bookmarks (endpoint_id, folder, notes) VALUES (?, ?, ?)", bookmarks_batch)
            cur.executemany("INSERT OR IGNORE INTO tags (endpoint_id, tag) VALUES (?, ?)", tags_batch)
            cur.executemany("INSERT INTO history (endpoint_id, action, details) VALUES (?, ?, ?)", history_batch)
            conn.commit()
            endpoints_batch.clear()
            bookmarks_batch.clear()
            tags_batch.clear()
            history_batch.clear()
            elapsed = time.time() - start_time
            print(f"    -> {i:,} / {count:,} records generated ({elapsed:.2f}s elapsed)")

    conn.close()
    total_time = time.time() - start_time
    _, peak_mem = tracemalloc.get_traced_memory()
    tracemalloc.stop()
    peak_mem_mb = peak_mem / (1024 * 1024)
    file_size_mb = os.path.getsize(db_path) / (1024 * 1024)
    print(f"[+] Complete! Generated {count:,} endpoints in {total_time:.2f}s ({count / total_time:,.0f} rows/s).")
    print(f"[+] Peak memory used: {peak_mem_mb:.2f} MB")
    print(f"[+] Database file size: {file_size_mb:.2f} MB ({db_path})")

def main():
    parser = argparse.ArgumentParser(description="EDMS Synthetic Data Generator")
    parser.add_argument("--count", type=int, default=5000, help="Number of endpoints to generate (e.g. 1000, 5000, 10000, 50000, 100000)")
    parser.add_argument("--output-dir", type=str, default="./benchmarks/data", help="Output directory for SQLite DB and storage")
    parser.add_argument("--db-name", type=str, default="edms_synthetic.db", help="Name of SQLite database file")
    parser.add_argument("--with-qp", action="store_true", help="Generate physical globalEQPData directories and JSON files")
    args = parser.parse_args()

    generate_data(args.count, args.output_dir, args.db_name, args.with_qp)

if __name__ == "__main__":
    main()
