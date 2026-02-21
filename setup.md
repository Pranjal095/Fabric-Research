# Distributed End-to-End Testing Setup Guide

This guide provides instructions to deploy and execute the Hyperledger Fabric Sharded evaluation experiments across a 3-machine distributed environment (1 Server, 2 VMs), and to configure a centralized CouchDB instance for storing benchmark analytics.

## 1. Environment Topology

You will deploy Fabric and the analytics engine across three machines. Ensure these machines can communicate with each other. If they are on different subnets or behind NAT, it is highly recommended to use **Tailscale** or **WireGuard** to create a flat overlay network.

**Machine Roles & IPs (Example tailnet IPs):**
*   **Machine 1 (Server - 100.x.x.1):** Orderer + Peers 1 to 3 (with their local CouchDB state databases)
*   **Machine 2 (VM 1 - 100.x.x.2):** Peers 4 to 7 (with their local CouchDB state databases)
*   **Machine 3 (VM 2 - 100.x.x.3):** Benchmark Client (Load Generator) + Centralized Analytics CouchDB

---

## 2. Prerequisites (All Machines)

1.  **OS:** Linux (Ubuntu 20.04+ recommended)
2.  **Go:** Version 1.20+ (`go version`)
3.  **Docker:** `docker` and `docker-compose` installed.
4.  **Source Code:** Clone the project repository to `$GOPATH/src/github.com/hyperledger/fabric` on all machines.
5.  **Python 3:** Required for docker-compose generation and uploading analytics. Install the `requests` library via `pip install requests` on Machine 3.

---

## 3. Configuration & Startup

### Step 3.1: Building Binaries (All Machines)
On all three machines, navigate to the fabric directory and compile the necessary binaries:
```bash
make orderer
make peer
go build -o deploy/benchmark_client ./cmd/benchmark_client/
```

### Step 3.2: Analytics Backend Setup (Machine 3 - VM 2)
On Machine 3, start the centralized CouchDB instance which will collect all the runtime metrics from the experiments.
```bash
cd deploy/analytics
docker-compose -f docker-compose-analytics.yaml up -d
```
*Note: This CouchDB instance will be available on port `5984` and the Fauxton UI can be accessed at `http://100.x.x.3:5984/_utils/`.*

### Step 3.3: Generating Fabric Nodes (Machine 1 & Machine 2)
We use the python generator script to avoid port conflicts and configure local CouchDB state databases for each peer.

**On Machine 1 (Server):**
```bash
cd deploy/
python3 generate_docker_compose.py --peers 3 --server 1
# This generates docker-compose-server1.yaml
docker-compose -f docker-compose-server1.yaml up -d
```

**On Machine 2 (VM 1):**
```bash
cd deploy/
python3 generate_docker_compose.py --peers 4 --server 2
# This generates docker-compose-server2.yaml
docker-compose -f docker-compose-server2.yaml up -d
```

### Step 3.4: Distributed Channel & Distinct Smart Contracts Setup
To simulate real sharding behaviour, distinct smart contracts must be used as independent shards. 

From **Machine 3 (VM 2)**, use standard Fabric CLI to create the channel against `100.x.x.1:7050` and join all 7 peers across Machine 1 and Machine 2.

Deploy the 7 standard samples as distinct chaincodes on the channel:
1.  `fabcar` (Shard 0)
2.  `marbles` (Shard 1)
3.  `smallbank` (Shard 2)
4.  `asset-transfer-basic` (Shard 3)
5.  `token-erc20` (Shard 4)
6.  `commercial-paper` (Shard 5)
7.  `auction` (Shard 6)

*Note: The cluster topology mapped out in `sharding.json` will need to be configured correctly with the IP addresses of the peers across Machine 1 and 2.*

---

## 4. Running the Benchmark Experiments

All 5 evaluation benchmarks from the `Experiment List` are automated in the `run_experiments.sh` script.

**On Machine 3 (VM 2):**
Ensure the `benchmark_client` binary is compiled and accessible in the `deploy/` directory.

The script accepts an argument to define the version of Fabric being evaluated (`proposed` or `vanilla`). This is purely used to tag the analytics data in CouchDB.

```bash
cd deploy/
chmod +x run_experiments.sh

# Edit run_experiments.sh to uncomment the specific experiment loops you wish to run

# To run tests against the Proposed (Dependency-Aware) Fabric build:
./run_experiments.sh proposed

# To run tests against the Vanilla Fabric build:
./run_experiments.sh vanilla
```

### Analytics Output
After every benchmark run defined inside the loops of `run_experiments.sh`, the results log is automatically parsed and sent to the centralized CouchDB instance on Machine 3. You can review the compiled JSON data directly through the Fauxton UI (`http://<Machine-3-IP>:5984/_utils/`) inside the `fabric_analytics` database.
