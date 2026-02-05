# Distributed Shard Cluster Deployment

This guide explains how to deploy the Sharded Raft Experiment on 3 servers with 15 nodes total.

## Prerequisites
- 3 Linux Servers (Server A, Server B, Server C).
- Network connectivity between them (TCP ports 7001-7005 open).
- `shard-server` binary (built from `go build ./cmd/shard-server`).

## Step 1: Generate Configuration
On your local machine or one server, run the generation script:

```bash
python3 deploy/generate_config.py
```
Enter the IP addresses of your servers when prompted.
It will create `cluster.json`.

## Step 2: Distribute Config and Binary
Copy `shard-server` and `cluster.json` to all 3 servers.

## Step 3: Run Nodes
### Server A (Nodes 1-5)
Run these commands in separate terminals or as background processes:
```bash
./shard-server -id 1 -config cluster.json &
./shard-server -id 2 -config cluster.json &
./shard-server -id 3 -config cluster.json &
./shard-server -id 4 -config cluster.json &
./shard-server -id 5 -config cluster.json &
```

### Server B (Nodes 6-10)
```bash
./shard-server -id 6 -config cluster.json &
./shard-server -id 7 -config cluster.json &
./shard-server -id 8 -config cluster.json &
./shard-server -id 9 -config cluster.json &
./shard-server -id 10 -config cluster.json &
```

### Server C (Nodes 11-15)
```bash
./shard-server -id 11 -config cluster.json &
./shard-server -id 12 -config cluster.json &
./shard-server -id 13 -config cluster.json &
./shard-server -id 14 -config cluster.json &
./shard-server -id 15 -config cluster.json &
```

## Step 4: Verify
Check the logs. You should see leader election occurring once a quorum (8 nodes) is up.
Leader log: `INFO: zzz became leader at term X`
Follower log: `INFO: zzz became follower at term X`
