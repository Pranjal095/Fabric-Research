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
Since `ORDERER_GENERAL_BOOTSTRAPMETHOD=none`, the orderer has no system channel. Tell the Orderer (running at `192.168.50.54:7050`) to create `mychannel` using the `osnadmin` tool compiled directly on your host. Because the Orderer's admin port (7053) requires mutual TLS, you must pass the Admin's TLS certificates:
```bash
../build/bin/osnadmin channel join --channelID mychannel --config-block ./mychannel.block -o 127.0.0.1:7053 --ca-file ./crypto-config/ordererOrganizations/example.com/orderers/orderer.example.com/tls/ca.crt --client-cert ./crypto-config/ordererOrganizations/example.com/users/Admin@example.com/tls/client.crt --client-key ./crypto-config/ordererOrganizations/example.com/users/Admin@example.com/tls/client.key
```

**2. Automatically Join Peers and Deploy Chaincode Shards**
Prior versions of this guide utilized outdated Fabric 1.x command structures (`peer chaincode deploy`) and necessitated logging into individual Docker containers. In reality, deploying 7 distinct Smart Contract Shards utilizing the Fabric v2.x lifecycle requires dozens of discrete actions spanning packaging, installation, organizational approvals, and channel commits securely via Mutual TLS.

To prevent manual errors and guarantee that a valid, cross-shard susceptible chaincode exists for the Endorser to trace during the Caliper workload, execute the fully automated `setup_all.sh` deployment script directly from your host machines (no Docker Exec required).

This script dynamically discovers all peers running on the local machine natively, joins them to `mychannel`, and properly installs the 7 chaincode shards utilizing the `cross_shard/chaincode.go` dependency logic required for `pcross` tracking.

**Since you split your 7 peers across two physical machines (Server 1 and VM 1), you MUST run this script independently on each machine!**

*On Server 1 (Host):*
```bash
cd deploy/
chmod +x setup_all.sh
./setup_all.sh
```
*(This will deploy to Peers 0, 1, 2, and then securely commit the chaincode definitions to the Orderer).*

*On VM 1 (IP 10.96.1.87):*
```bash
cd deploy/
chmod +x setup_all.sh
./setup_all.sh
```
*(This will discover and deploy to Peers 3, 4, 5, 6 so they don't crash when Caliper routes transactions to them).*

### Step 3.5: Configuring Shard Sizes (Optional)
To alter the cluster size for specific experiments (e.g., a cluster of 3 vs 5), edit the `sharding.json` topology map located in the peer's filesystem path before starting the benchmarks. The peers will hot-reload the Raft configurations. If you are running with the default 7 active shards deployed in Step 3.3, you do not need to do this.

---

## 4. Running the Benchmark Experiments (Hyperledger Caliper)

To obtain natively executed, publishable results for the Dependency-Aware architecture evaluation, this network utilizes **Hyperledger Caliper** rather than mock simulators.

Caliper will generate massive, multi-threaded gRPC cryptographic loads directly against the distinct Smart Contract shards we deployed in Step 3.4.

### Step 4.1: Install Caliper on Machine 3 (Client)
Ensure you have Node.js (v18+) and npm installed on Machine 3. Then, install the Caliper CLI:
```bash
npm install -g @hyperledger/caliper-cli@0.5.0
```

Because we are targeting Hyperledger Fabric v2.5.x, explicitly bind the Caliper Fabric adapter before running:
```bash
caliper bind --caliper-bind-sut fabric:2.4
```
*(Caliper 0.5.0 uses the 2.4 adapter for all 2.x Fabric networks).*

### Step 4.2: Execute the Native Benchmarks
We have configured a comprehensive `caliper-workspace` in the `deploy/` directory that natively targets the 3-VM architecture and contains the dynamic `pcross` custom JavaScript workload generator.

**On Machine 3 (Client):**
```bash
cd deploy/caliper-workspace

# Run EXPERIMENT 6 (Throughput vs Pcross)
caliper launch manager \
  --caliper-workspace . \
  --caliper-networkconfig network-config.yaml \
  --caliper-benchconfig benchmarks/config.yaml \
  --caliper-flow-only-test
```

## 5. Extracting Evaluation Statistics

Caliper will directly output a beautiful, publishable HTML report (`report.html`) in your workspace directory when the execution finishes. This report details the *true* hardware metrics of the peer network:
*   **Send Rate (TPS)**
*   **Max/Min/Avg Latency (ms)**
*   **Throughput (TPS)**
*   **Successful/Failed Transactions** 

Use these raw network I/O numbers to plot the curves for the final thesis evaluation!
