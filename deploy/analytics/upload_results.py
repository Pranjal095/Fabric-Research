#!/usr/bin/env python3
import sys
import re
import requests
import json
import datetime
import uuid

# Configuration
COUCHDB_URL = "http://127.0.0.1:5984"
COUCHDB_USER = "admin"
COUCHDB_PASS = "adminpw"
DB_NAME = "fabric_analytics"

def setup_db():
    auth = (COUCHDB_USER, COUCHDB_PASS)
    try:
        req = requests.put(f"{COUCHDB_URL}/{DB_NAME}", auth=auth)
        if req.status_code in [201, 202]:
            print(f"Created Database '{DB_NAME}'")
    except requests.exceptions.ConnectionError:
        print(f"Error: Could not connect to CouchDB at {COUCHDB_URL}. Make sure it is running.")
        sys.exit(1)

def parse_log_file(filepath, fabric_version):
    # Log parsing regex
    params_rx = re.compile(r"Load Parameters\s*:\s*(\d+)\s*Txs\s*\|\s*([\d\.]+)%\s*Dependency\s*\|\s*(\d+)\s*Threads")
    tp_rx = re.compile(r"\[METRICS\]\s*Throughput:\s*([\d\.]+)\s*TPS")
    rr_rx = re.compile(r"\[METRICS\]\s*RejectRate:\s*([\d\.]+)%")
    ar_rx = re.compile(r"\[METRICS\]\s*AvgResponse:\s*([\d\.]+)ms")

    data = {
        "id": str(uuid.uuid4()),
        "timestamp": datetime.datetime.utcnow().isoformat() + "Z",
        "fabric_version": fabric_version,
        "log_file": filepath,
        "tx_count": 0,
        "dependency_rate": 0.0,
        "threads": 0,
        "throughput": 0.0,
        "reject_rate": 0.0,
        "avg_response_time": 0.0
    }

    try:
        with open(filepath, 'r') as f:
            for line in f:
                params_match = params_rx.search(line)
                if params_match:
                    data["tx_count"] = int(params_match.group(1))
                    data["dependency_rate"] = float(params_match.group(2))
                    data["threads"] = int(params_match.group(3))

                tp_match = tp_rx.search(line)
                if tp_match:
                    data["throughput"] = float(tp_match.group(1))

                rr_match = rr_rx.search(line)
                if rr_match:
                    data["reject_rate"] = float(rr_match.group(1))

                ar_match = ar_rx.search(line)
                if ar_match:
                    data["avg_response_time"] = float(ar_match.group(1))
    except FileNotFoundError:
        print(f"Error: Log file '{filepath}' not found.")
        return None

    return data

def upload_to_couchdb(doc):
    if not doc:
        return
    auth = (COUCHDB_USER, COUCHDB_PASS)
    headers = {'Content-type': 'application/json'}
    url = f"{COUCHDB_URL}/{DB_NAME}"
    response = requests.post(url, data=json.dumps(doc), headers=headers, auth=auth)
    
    if response.status_code in [201, 202]:
        print(f"Successfully uploaded analytics doc: {response.json()['id']}")
    else:
        print(f"Failed to upload document: {response.status_code} - {response.text}")

if __name__ == "__main__":
    if len(sys.argv) < 3:
        print("Usage: python3 upload_results.py <path_to_log_file> <fabric_version|proposed|vanilla>")
        sys.exit(1)

    log_filepath = sys.argv[1]
    fabric_version = sys.argv[2]

    setup_db()
    
    parsed_data = parse_log_file(log_filepath, fabric_version)
    if parsed_data:
        upload_to_couchdb(parsed_data)
