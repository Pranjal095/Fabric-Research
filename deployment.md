# Real-World Deployment & Evaluation Guide (3-Machine Setup)

This guide details how to deploy your custom Dependency-Aware Hyperledger Fabric build across three machines (2 VMs, 1 Server) in the same network to run the exact performance benchmarks required for the evaluation.

Unlike previous versions that relied on in-process simulators or duplicate contract names, this architecture uses **Docker Compose**, real **CouchDB** instances, and **distinct Smart Contracts** to emulate true sharding and Raft consensus.

## 1. Network Architecture & Connectivity
Since the machines (2 VMs, 1 Server) are in the same network, they can communicate with each other directly. No overlay network (like Tailscale) is required.

**Roles:**
*   **Machine 1 (Server):** Orderer + Peers 1-3. IP: `192.168.x.1`
*   **Machine 2 (VM 1):** Peers 4-7. IP: `192.168.x.2`
*   **Machine 3 (VM 2):** Benchmark Client (Load Generator). IP: `192.168.x.3`

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

**On Machine 2 (VM 1):**
```bash
python3 deploy/generate_docker_compose.py --peers 4 --server 2
# This generates `docker-compose-server2.yaml` (Ports 7051, 8051, 9051, 10051)
docker-compose -f docker-compose-server2.yaml up -d
```

### Step 3.3: Distributed Channel & Distinct Smart Contracts Setup
To utilize proper sharding logic, you **must deploy distinct smart contracts** to represent different logical state partitions (Shards).

From **Machine 3 (Client - 10.96.0.221)**, use the standard Fabric CLI to create the channel and join all 7 peers across the Server and VM1.

**1. Create the Channel (Connecting to Server's Orderer):**
```bash
osnadmin channel join --channelID mychannel --config-block ./mychannel.block -o 192.168.50.54:7050
```

**2. Join Peers to the Channel:**
```bash
# Join Server Peers (192.168.50.54)
peer channel join -b mychannel.block -o 192.168.50.54:7050 --peerAddress 192.168.50.54:7051
peer channel join -b mychannel.block -o 192.168.50.54:7050 --peerAddress 192.168.50.54:8051
peer channel join -b mychannel.block -o 192.168.50.54:7050 --peerAddress 192.168.50.54:9051

# Join VM1 Peers (10.96.1.87)
peer channel join -b mychannel.block -o 192.168.50.54:7050 --peerAddress 10.96.1.87:7051
peer channel join -b mychannel.block -o 192.168.50.54:7050 --peerAddress 10.96.1.87:8051
peer channel join -b mychannel.block -o 192.168.50.54:7050 --peerAddress 10.96.1.87:9051
peer channel join -b mychannel.block -o 192.168.50.54:7050 --peerAddress 10.96.1.87:10051
```

**3. Deploy the 7 standard samples as distinct chaincodes:**
```bash
# Deploy to Server (192.168.50.54)
peer chaincode deploy -n fabcar -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/fabcar/go -c '{"Args":[]}' -o 192.168.50.54:7050 --peerAddress 192.168.50.54:7051
peer chaincode deploy -n marbles -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/marbles02/go -c '{"Args":[]}' -o 192.168.50.54:7050 --peerAddress 192.168.50.54:8051
peer chaincode deploy -n smallbank -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/smallbank/go -c '{"Args":[]}' -o 192.168.50.54:7050 --peerAddress 192.168.50.54:9051

# Deploy to VM1 (10.96.1.87)
peer chaincode deploy -n asset-transfer-basic -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/asset-transfer-basic/go -c '{"Args":[]}' -o 192.168.50.54:7050 --peerAddress 10.96.1.87:7051
peer chaincode deploy -n token-erc20 -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/token-erc20/go -c '{"Args":[]}' -o 192.168.50.54:7050 --peerAddress 10.96.1.87:8051
peer chaincode deploy -n commercial-paper -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/commercial-paper/go -c '{"Args":[]}' -o 192.168.50.54:7050 --peerAddress 10.96.1.87:9051
peer chaincode deploy -n auction -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/auction/go -c '{"Args":[]}' -o 192.168.50.54:7050 --peerAddress 10.96.1.87:10051
```

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
