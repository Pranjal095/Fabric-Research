# Real-World Deployment Guide (3-Machine Setup)

This guide details how to deploy your custom Hyperledger Fabric build across three physical machines (2 Laptops, 1 Server on different subnet) to run the performance benchmark.

## 1. Network Architecture & Connectivity
Since the machines are on different subnets (and likely behind NAT), direct communication requires a **Virtual Private Network (VPN)** or **Overlay Network**.

### Recommended Setup: **Tailscale / WireGuard**
1.  **Install Tailscale** on all 3 machines (`curl -fsSL https://tailscale.com/install.sh | sh`).
2.  **Authenticate** and ensure they appear in the same tailnet.
3.  **Use Tailscale IPs**: Use the `100.x.y.z` IP addresses assigned by Tailscale for all Fabric configurations. This flattens the network and bypasses NAT/Subnet issues.

**Roles:**
*   **Machine 1 (Server):** Orderer (Runs Raft Consensus). IP: `100.x.x.1`
*   **Machine 2 (Laptop A):** Peer (Endorser/Committer). IP: `100.x.x.2`
*   **Machine 3 (Laptop B):** Client (Benchmark Runner). IP: `100.x.x.3`

## 2. Prerequisites (All Machines)
1.  **OS:** Linux (Ubuntu 20.04+ recommended)
2.  **Go:** Version 1.20+ (`go version`)
3.  **Source Code:** Clone `Mini-Project-1` to `$GOPATH/src/github.com/hyperledger/fabric`.

---

## 3. Configuration & Startup

### Step 3.1: Orderer (Machine 1)
1.  **Edit `orderer.yaml`**:
    *   `General.ListenAddress`: `0.0.0.0`
    *   `General.ListenPort`: `7050`
    *   `General.TLS.Enabled`: `false` (for simplicity)
2.  **Start Orderer**:
    ```bash
    make orderer
    ORDERER_GENERAL_LISTENADDRESS=0.0.0.0 ./build/bin/orderer start
    ```

### Step 3.2: Peer (Machine 2) - Sharding Enabled
1.  **Edit `core.yaml`**:
    *   `peer.address`: `100.x.x.2:7051` (Tailscale IP)
    *   `peer.gossip.bootstrap`: `100.x.x.2:7051`
    *   `peer.gossip.externalEndpoint`: `100.x.x.2:7051`
2.  **Enable Sharding**:
    *   Ensure `experimental.sharding.enabled: true` in `core.yaml` or set env var.
3.  **Start Peer**:
    ```bash
    make peer
    CORE_PEER_ADDRESS=100.x.x.2:7051 ./build/bin/peer node start
    ```

### Step 3.3: Channel Setup (Machine 3 - Client)
1.  **Create Channel**:
    ```bash
    export CORE_PEER_ADDRESS=100.x.x.2:7051
    ./build/bin/peer channel create -o 100.x.x.1:7050 -c mychannel -f mychannel.tx
    ```
2.  **Join Peer**:
    *   Copy `mychannel.block` to Machine 2.
    *   Run on Machine 2: `./build/bin/peer channel join -b mychannel.block`

---

## 4. Deploying for Sharding (Multi-Contract)
To utilize the sharding logic, you must deploy **multiple smart contracts**. The `ShardManager` assigns different contracts to different shards (Raft groups).

### Step 4.1: Deploy Multiple Chaincodes
Deploy 3 instances of `fabcar` with different names:
1.  `fabcar_0` -> Shard 0
2.  `fabcar_1` -> Shard 1
3.  `fabcar_2` -> Shard 2

On Machine 3 (Client):
```bash
# Package & Install (on Machine 2)
# ... standard lifecycle commands ...

# Approve & Commit for fabcar_0
./build/bin/peer lifecycle chaincode commit -o 100.x.x.1:7050 --channelID mychannel --name fabcar_0 ...

# Approve & Commit for fabcar_1
./build/bin/peer lifecycle chaincode commit -o 100.x.x.1:7050 --channelID mychannel --name fabcar_1 ...
```

## 5. Configuring Cluster Size (Replicas)
"Cluster Size" refers to the **Raft Replica Count** for each shard.
*   **Method**: This is configured in the Sharding Policy or `core.yaml` depending on your implementation.
*   **Constraint**: With only 3 machines, a `ClusterSize=3` means each machine runs 1 replica for that shard (if you run Peer instances on all machines).
*   **Simulation**: To test `ClusterSize=5` on 3 machines, you would need to run multiple Peer processes on the Server/Laptops on different ports (e.g., 7051, 8051, 9051).

## 6. Running the Benchmark
Use the custom client to target multiple contracts.

```bash
# Run benchmark targeting 3 contracts with 32 threads
go run cmd/benchmark_client/main.go \
  --peer 100.x.x.2:7051 \
  --orderer 100.x.x.1:7050 \
  --txs 5000 \
  --cc_base fabcar \
  --cc_count 3
```

This ensures transactions are distributed:
- Tx 1 -> `fabcar_0` (Shard A)
- Tx 2 -> `fabcar_1` (Shard B)
- Tx 3 -> `fabcar_2` (Shard C)

## 7. Advanced: Custom Sharding & Multiple Peers per Machine (Cluster Size > 3)

To simulate larger clusters (e.g., 5 replicas) on just 3 machines, or to customize the topology, follow these steps.

### 7.1 Create `sharding.json`
Create a file named `sharding.json` in the working directory of **each Peer**.
This file maps the chaincode name to the list of replica addresses for its shard.

**Example `sharding.json`:**
```json
{
  "fabcar_0": [
    "100.x.x.2:7051",
    "100.x.x.2:8051",
    "100.x.x.2:9051", 
    "100.x.x.1:7051", 
    "100.x.x.3:7051"
  ],
  "fabcar_1": [ ... ]
}
```
*   Configures a 5-node Raft cluster for `fabcar_0`.
*   Includes ports 7051, 8051, 9051 on Machine 2 (IP `.2`).

### 7.2 Running Multiple Peers on One Machine
To run additional peers (e.g., on ports 8051 and 9051) on Machine 2:

**Peer 2 (Port 8051):**
```bash
# Terminal 2 on Machine 2
export CORE_PEER_ID=peer2
export CORE_PEER_ADDRESS=100.x.x.2:8051
export CORE_PEER_LISTENADDRESS=0.0.0.0:8051
export CORE_PEER_CHAINCODELISTENADDRESS=0.0.0.0:8052
export CORE_PEER_GOSSIP_EXTERNALENDPOINT=100.x.x.2:8051
export CORE_PEER_FILESYSTEMPATH=/var/hyperledger/production/peer2
export CORE_LEDGER_STATE_STATEDATABASE=CouchDB
export CORE_LEDGER_STATE_COUCHDBCONFIG_COUCHDBADDRESS=localhost:6984 # Needs separate CouchDB or use GoLevelDB

# Start Peer
./build/bin/peer node start
```

**Peer 3 (Port 9051):**
```bash
# Terminal 3 on Machine 2
export CORE_PEER_ID=peer3
export CORE_PEER_ADDRESS=100.x.x.2:9051
# ... adjust ports and filesystem path ...
./build/bin/peer node start
```

### 7.3 Cluster Formation
1.  When `fabcar_0` is invoked, the `ShardManager` on each peer reads `sharding.json`.
2.  It compares its `CORE_PEER_ADDRESS` to the list.
    *   Peer 1 (7051) sees it is index 0 -> ReplicaID 1
    *   Peer 2 (8051) sees it is index 1 -> ReplicaID 2
3.  They form a Raft cluster.


