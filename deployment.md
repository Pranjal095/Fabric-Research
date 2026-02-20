# Real-World Deployment & Evaluation Guide (3-Machine Setup)

This guide details how to deploy your custom Dependency-Aware Hyperledger Fabric build across three physical machines (2 Laptops, 1 Server on different subnets) to run the exact performance benchmarks required for the evaluation.

Unlike previous versions that relied on in-process simulators or duplicate contract names, this architecture uses **Docker Compose**, real **CouchDB** instances, and **distinct Smart Contracts** to emulate true sharding and Raft consensus.

## 1. Network Architecture & Connectivity
Since the machines are on different subnets (and likely behind NAT), direct communication requires an Overlay Network.

### Recommended Setup: **Tailscale / WireGuard**
1.  **Install Tailscale** on all 3 machines (`curl -fsSL https://tailscale.com/install.sh | sh`).
2.  **Authenticate** and ensure they appear in the same tailnet.
3.  **Use Tailscale IPs**: Use the `100.x.y.z` IP addresses assigned by Tailscale for all Fabric configurations. This flattens the network and bypasses NAT/Subnet issues.

**Roles:**
*   **Machine 1 (Server):** Orderer + Peers 1-3. IP: `100.x.x.1`
*   **Machine 2 (Laptop A):** Peers 4-7. IP: `100.x.x.2`
*   **Machine 3 (Laptop B):** Benchmark Client (Load Generator). IP: `100.x.x.3`

## 2. Prerequisites (All Machines)
1.  **OS:** Linux (Ubuntu 20.04+ recommended)
2.  **Go:** Version 1.20+ (`go version`)
3.  **Docker:** `docker` and `docker-compose` installed.
4.  **Source Code:** Clone `Mini-Project-1` to `$GOPATH/src/github.com/hyperledger/fabric`.

---

## 3. Configuration & Startup

### Step 3.1: Building the Binaries
On all machines, ensure the updated binaries are compiled:
```bash
make orderer
make peer
go build -o deploy/benchmark_client ./cmd/benchmark_client/
```

### Step 3.2: Generating the Physical Network
To deploy multiple peers (with individual CouchDB instances) on a single physical machine without port conflicts, utilize the Python generator script.

**On Machine 1 (Server):**
```bash
python3 deploy/generate_docker_compose.py --peers 3 --server 1
# This generates `docker-compose-server1.yaml` (Ports 7051, 8051, 9051)
docker-compose -f docker-compose-server1.yaml up -d
```

**On Machine 2 (Laptop A):**
```bash
python3 deploy/generate_docker_compose.py --peers 4 --server 2
# This generates `docker-compose-server2.yaml` (Ports 7051, 8051, 9051, 10051)
docker-compose -f docker-compose-server2.yaml up -d
```

### Step 3.3: Distributed Channel & Distinct Smart Contracts Setup
To utilize proper sharding logic, you **must deploy distinct smart contracts** to represent different logical state partitions (Shards). **Do not deploy the same contract under different names.**

From **Machine 3 (Client)**, run the standard CLI commands to create the channel against `100.x.x.1:7050` and join all 7 peers. 

Then, deploy the following 7 standard samples as distinct chaincodes:
1.  `fabcar` (Shard 0)
2.  `marbles` (Shard 1)
3.  `smallbank` (Shard 2)
4.  `asset-transfer-basic` (Shard 3)
5.  `token-erc20` (Shard 4)
6.  `commercial-paper` (Shard 5)
7.  `auction` (Shard 6)

### Step 3.4: Configuring Shard Sizes
To alter the cluster size for specific experiments (e.g., a cluster of 3 vs 5), edit the `sharding.json` topology map located in the peer's filesystem path before starting the benchmarks. The peers will hot-reload the Raft configurations.

---

## 4. Running the Benchmark Experiments

All evaluation benchmarks are automated via the `run_experiments.sh` wrapper script. This script invokes the real `benchmark_client` and passes varying transaction counts, thread sizes, cluster sizes, and dependency rates.

**On Machine 3 (Client):**
```bash
cd deploy/
chmod +x run_experiments.sh
./run_experiments.sh
```

### The 5 Standardized Experiments

The bash script handles the loop for the exact combinations required for your evaluation. Simply uncomment the required line at the bottom of the script.

1.  **EXP 1: Throughput and Reject Rate vs Tx Count**
    *   Variables: Txs (1000-5000), Dependency (40%), Threads (32), Cluster Size (3 or 5).
    *   Output: `results_EXP1_Cluster[N]_Tx[X].log`

2.  **EXP 2: Throughput and Reject Rate vs Dependency**
    *   Variables: Dependency (0%-50%), Txs (1000), Threads (32), Cluster Size (1).
    *   Output: `results_EXP2_Dep[N].log`

3.  **EXP 3: Throughput and Reject Rate vs Threads**
    *   Variables: Threads (1, 2, 4, 8, 16, 32), Txs (1000), Dep (40%), Cluster Size (3).
    *   Output: `results_EXP3_Threads[N].log`

4.  **EXP 4: Throughput and Reject Rate vs Cluster Size**
    *   Variables: Cluster Size (1, 3, 5, 7), Txs (1000), Dep (40%), Threads (32).
    *   *Note: Ensure `sharding.json` is updated with all 7 nodes across Machines 1 & 2 before running this specific loop.*
    *   Output: `results_EXP4_Cluster[N].log`

5.  **EXP 5: Response Time vs Transactions**
    *   Variables: Txs (1000-5000), Dep (40%), Threads (32), Cluster (3).
    *   Provides explicit terminal metrics on Overall Avg Response Time, Validate/Commit Time, and Abort Response Time.
    *   Output: `results_EXP5_Latency_Tx[N].log`

## 5. Extracting Evaluation Statistics

After the `./run_experiments.sh` script completes, the results are logged automatically.

**Example Log Output (`results_EXP1_Cluster3_Tx1000.log`):**
```
--- BENCHMARK CLIENT EXECUTION ---
Routing Targets : Peer=localhost:7051 | Orderer=localhost:7050
Load Parameters : 1000 Txs | 40.00% Dependency | 32 Threads
Active Shards   : 7 (fabcar, marbles, smallbank, asset-transfer-basic, token-erc20, commercial-paper, auction)
----------------------------------
Distributing transactions across independent chaincode shards...
Done in 2.19034s
[METRICS] Throughput: 456.55 TPS
[METRICS] RejectRate: 40.00%
[METRICS] AvgResponse: 2.19ms
```

You can `grep` these text files locally on the Client machine to extract the exact CSV arrays needed to plot the 2D evaluation curves (Using the internal `scripts/plot_benchmark.py` tooling if desired).
