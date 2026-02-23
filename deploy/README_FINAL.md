# Final Distributed Experiment Guide

This guide explains how to deploy and run the Sharded Raft Experiment on 3 machines (e.g., 2 VMs + 1 Server).

> [!IMPORTANT]
> **Network Connectivity Requirement**  
> Before starting, ensure all 3 machines can reach each other.  
> Run `ping <Machine_IP>` from each machine to the others.  
> If you see `Time to live exceeded` or `Destination Host Unreachable`, **you cannot proceed**.  
> **Solution:** Ensure the VMs and the Server are connected to the same local network and can communicate directly without firewalls. 

## 1. Prerequisites
- **Go 1.19+** on all machines.
- **Network Access:** All machines must be on the same subnet (e.g., `192.168.1.x`) or have proper routing.
- **Ports 7001-7015** must be open (check firewalls).

## 2. Build the Binaries
On your development machine, compile the server and experiment runner:

```bash
# Build the Shard Server node
go build -o shard-server ./cmd/shard-server

# Build the Experiment Runner
go build -o experiment ./cmd/experiment
```

## 3. Generate Configuration
Run the helper script to create `cluster.json`:

```bash
python3 deploy/generate_config.py
```
- Enter the IP addresses for Server 1, Server 2, and Server 3 when prompted.
- This creates `cluster.json` with unique ports for each node.

## 4. Distribute Files
Copy `shard-server`, `experiment`, and `cluster.json` to **ALL 3 machines**.
You can use `scp` (replace user/ip with yours):

```bash
scp shard-server experiment cluster.json user@192.168.x.x:~/
```

## 5. Run the Nodes
SSH into each machine and run the corresponding nodes.

### Machine 1 (Nodes 1-5)
```bash
./shard-server -id 1 -config cluster.json &
./shard-server -id 2 -config cluster.json &
./shard-server -id 3 -config cluster.json &
./shard-server -id 4 -config cluster.json &
./shard-server -id 5 -config cluster.json &
```

### Machine 2 (Nodes 6-10)
```bash
./shard-server -id 6 -config cluster.json &
./shard-server -id 7 -config cluster.json &
./shard-server -id 8 -config cluster.json &
./shard-server -id 9 -config cluster.json &
./shard-server -id 10 -config cluster.json &
```

### Machine 3 (Nodes 11-15)
```bash
./shard-server -id 11 -config cluster.json &
./shard-server -id 12 -config cluster.json &
./shard-server -id 13 -config cluster.json &
./shard-server -id 14 -config cluster.json &
./shard-server -id 15 -config cluster.json &
```

> **Verify:** Check logs for `Leader elected` or `Follower` messages. If you see `connection refused`, ensure other machines are running!

## 6. Run the Experiment
Once all 15 nodes are running (even without a leader elected yet), run the experiment from **ONE** machine (e.g., Machine 1):

```bash
./experiment -config cluster.json -load 1000
```
This will send 1000 transactions to the cluster and measure performance.

## Troubleshooting
- **Bind Error:** `bind: cannot assign requested address` -> You are running a node on an incorrect machine. Check `cluster.json` IP for that ID.
- **Connection Refused:** The target node is not running or firewall is blocking the port.
- **No Route to Host:** Network isolation issue. Switch to a common Wi-Fi/Hotspot.
