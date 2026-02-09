# End-to-End Distributed Experiment Guide

This guide explains how to deploy and run the Sharded Raft Experiment across 3 servers using the provided deployment scripts.

## 1. Build the Server Binary
On your development machine, build the enhanced shard server:

```bash
go build -o shard-server ./cmd/shard-server
```

## 2. Generate Cluster Configuration
Use the Python script to generate the `cluster.json` topology file.

```bash
python3 deploy/generate_config.py
```

*   **Prompt 1**: Enter IP for Server 1
*   **Prompt 2**: Enter IP for Server 2
*   **Prompt 3**: Enter IP for Server 3

This will create `cluster.json` containing the peer mappings for 15 nodes.

## 3. Deployment
Copy the following files to **all 3 servers**:
1.  `shard-server` (The binary you built)
2.  `cluster.json` (The config you generated)

## 4. Execution Rules
*   **Server 1**: Runs Nodes 1-5
*   **Server 2**: Runs Nodes 6-10
*   **Server 3**: Runs Nodes 11-15

## 5. Running the Experiment

### Step 1: Start Follower Nodes
On **Server 1**, start nodes 1-5:
```bash
./shard-server -id 1 -config cluster.json &
./shard-server -id 2 -config cluster.json &
./shard-server -id 3 -config cluster.json &
./shard-server -id 4 -config cluster.json &
./shard-server -id 5 -config cluster.json &
```

On **Server 2**, start nodes 6-10:
```bash
./shard-server -id 6 -config cluster.json &
...
./shard-server -id 10 -config cluster.json &
```

On **Server 3**, start nodes 11-14:
```bash
./shard-server -id 11 -config cluster.json &
...
./shard-server -id 14 -config cluster.json &
```

### Step 2: Start Leader & Load Generator
On **Server 3**, start Node 15 with load generation enabled:

```bash
./shard-server -id 15 -config cluster.json -load 1000
```
*(This starts Node 15, waits for leader election, and then pumps 1000 transactions into the cluster)*

## 6. Monitoring & Results
*   Watch the output of Node 15.
*   It will report progress every 100 committed transactions.
*   **Final Result**: Look for `Workload completed! Throughput: X TPS`.
