# Real-World Deployment & Evaluation Guide (3-Machine Setup)

This guide details how to deploy your custom Dependency-Aware Hyperledger Fabric build across three machines (2 VMs, 1 Server) in the same network to run the exact performance benchmarks required for the evaluation.

Unlike previous versions that relied on in-process simulators or duplicate contract names, this architecture uses **Docker Compose**, real **CouchDB** instances, and **distinct Smart Contracts** to emulate true sharding and Raft consensus.

## 1. Network Architecture & Connectivity
Since the machines (2 VMs, 1 Server) are in the same network, they can communicate with each other directly. No overlay network (like Tailscale) is required.

**Roles:**
*   **Machine 1 (Server):** Orderer + Peers 0-2. IP: `192.168.x.1`
*   **Machine 2 (VM 1):** Peers 3-6. IP: `192.168.x.2`
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

### Step 3.2: Generating Crypto Materials & Genesis Block
Before the nodes can be started, you must generate their MSP certificates and the Orderer's genesis block. We have added a new script `generate_crypto.sh` to automate this.

**On Machine 1 (Server):**
```bash
cd deploy/
chmod +x generate_crypto.sh
./generate_crypto.sh
```
*(This will compile `cryptogen` and `configtxgen`, populate the `deploy/crypto-config` folder, and generate `mychannel.block`.)*

### Step 3.3: Generating the Physical Network
To deploy multiple peers (with individual CouchDB instances) on a single physical machine without port conflicts, utilize the Python generator script.

**On Machine 1 (Server):**
```bash
cd deploy/
python3 generate_docker_compose.py --peers 3 --start-peer 0 --server 1
# This generates `docker-compose-server1.yaml` (Ports 7051, 8051, 9051)
docker-compose -f docker-compose-server1.yaml up -d
```

**On Machine 2 (VM 1):**
```bash
cd deploy/
python3 generate_docker_compose.py --peers 4 --start-peer 3 --server 2
# This generates `docker-compose-server2.yaml` (Ports 7051, 8051, 9051, 10051)
docker-compose -f docker-compose-server2.yaml up -d
```

### Step 3.4: Distributed Channel & Distinct Smart Contracts Setup
To utilize proper sharding logic, you **must deploy distinct smart contracts** to represent different logical state partitions (Shards). The safest and most reliable way to execute these commands without local dependency/TLS issues is from *inside* one of the peer containers on the Server.

**1. Create the Channel on the Orderer (On Machine 1 - Server Host):**
Since `ORDERER_GENERAL_BOOTSTRAPMETHOD=none`, the orderer has no system channel. Tell the Orderer (running at `192.168.50.54:7050`) to create `mychannel` using the `osnadmin` tool compiled directly on your host:
```bash
../build/bin/osnadmin channel join --channelID mychannel --config-block ./mychannel.block -o 127.0.0.1:7053
```

**2. Enter the Peer Container (On Machine 1 - Server):**
SSH into Machine 1 and open an interactive shell inside Peer 0:
```bash
docker exec -it peer0.org1.example.com bash
```

**3. Set Base Environment Variables (Inside Container):**
Before running CLI commands, set the environment variables to use Org1's MSP:
```bash
export CORE_PEER_LOCALMSPID="Org1MSP"
export CORE_PEER_TLS_ENABLED=false
```

**4. Join the Server Peers (Machine 1) to the Channel:**
```bash
# Join Peer 0
export CORE_PEER_ADDRESS=192.168.50.54:7051
peer channel join -b mychannel.block

# Join Peer 1
export CORE_PEER_ADDRESS=192.168.50.54:8051
peer channel join -b mychannel.block

# Join Peer 2
export CORE_PEER_ADDRESS=192.168.50.54:9051
peer channel join -b mychannel.block
```

**5. Join the VM 1 Peers (Machine 2) to the Channel:**
```bash
# Join Peer 3
export CORE_PEER_ADDRESS=10.96.1.87:7051
peer channel join -b mychannel.block

# Join Peer 4
export CORE_PEER_ADDRESS=10.96.1.87:8051
peer channel join -b mychannel.block

# Join Peer 5
export CORE_PEER_ADDRESS=10.96.1.87:9051
peer channel join -b mychannel.block

# Join Peer 6
export CORE_PEER_ADDRESS=10.96.1.87:10051
peer channel join -b mychannel.block
```

**6. Package and Deploy Chaincodes to the Server (Machine 1):**
```bash
# Shard 0: Fabcar (Peer 0)
peer lifecycle chaincode package fabcar.tar.gz --path /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/fabcar/go/ --lang golang --label fabcar_1.0
export CORE_PEER_ADDRESS=192.168.50.54:7051
peer lifecycle chaincode install fabcar.tar.gz
peer chaincode deploy -n fabcar -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/fabcar/go -c '{"Args":[]}' -o 192.168.50.54:7050

# Shard 1: Marbles (Peer 1)
peer lifecycle chaincode package marbles.tar.gz --path /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/marbles02/go/ --lang golang --label marbles_1.0
export CORE_PEER_ADDRESS=192.168.50.54:8051
peer lifecycle chaincode install marbles.tar.gz
peer chaincode deploy -n marbles -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/marbles02/go -c '{"Args":[]}' -o 192.168.50.54:7050

# Shard 2: Smallbank (Peer 2)
peer lifecycle chaincode package smallbank.tar.gz --path /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/smallbank/go/ --lang golang --label smallbank_1.0
export CORE_PEER_ADDRESS=192.168.50.54:9051
peer lifecycle chaincode install smallbank.tar.gz
peer chaincode deploy -n smallbank -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/smallbank/go -c '{"Args":[]}' -o 192.168.50.54:7050
```

**7. Package and Deploy Chaincodes to VM 1 (Machine 2):**
```bash
# Shard 3: Asset Transfer (Peer 3)
peer lifecycle chaincode package asset.tar.gz --path /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/asset-transfer-basic/go/ --lang golang --label asset_1.0
export CORE_PEER_ADDRESS=10.96.1.87:7051
peer lifecycle chaincode install asset.tar.gz
peer chaincode deploy -n asset-transfer-basic -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/asset-transfer-basic/go -c '{"Args":[]}' -o 192.168.50.54:7050

# Shard 4: Token ERC20 (Peer 4)
peer lifecycle chaincode package token.tar.gz --path /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/token-erc20/go/ --lang golang --label token_1.0
export CORE_PEER_ADDRESS=10.96.1.87:8051
peer lifecycle chaincode install token.tar.gz
peer chaincode deploy -n token-erc20 -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/token-erc20/go -c '{"Args":[]}' -o 192.168.50.54:7050

# Shard 5: Commercial Paper (Peer 5)
peer lifecycle chaincode package paper.tar.gz --path /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/commercial-paper/go/ --lang golang --label paper_1.0
export CORE_PEER_ADDRESS=10.96.1.87:9051
peer lifecycle chaincode install paper.tar.gz
peer chaincode deploy -n commercial-paper -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/commercial-paper/go -c '{"Args":[]}' -o 192.168.50.54:7050

# Shard 6: Auction (Peer 6)
peer lifecycle chaincode package auction.tar.gz --path /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/auction/go/ --lang golang --label auction_1.0
export CORE_PEER_ADDRESS=10.96.1.87:10051
peer lifecycle chaincode install auction.tar.gz
peer chaincode deploy -n auction -p /opt/gopath/src/github.com/hyperledger/fabric-samples/chaincode/auction/go -c '{"Args":[]}' -o 192.168.50.54:7050
```

Once complete, `exit` the container shell.

### Step 3.5: Configuring Shard Sizes (Optional)
To alter the cluster size for specific experiments (e.g., a cluster of 3 vs 5), edit the `sharding.json` topology map located in the peer's filesystem path before starting the benchmarks. The peers will hot-reload the Raft configurations. If you are running with the default 7 active shards deployed in Step 3.3, you do not need to do this.

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
