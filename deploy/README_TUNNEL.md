# Distributed Deployment via SSH Tunnels

This method solves the "different subnet" problem.

## Architecture
- **Server C (192.168.50.54):** Acts as the central hub.
- **Laptops A & B:** Connect to Server C via SSH Tunnels.

## Step 1: Prepare Config Files
### On Laptops A & B
Use `deploy/cluster_laptops.json`. Rename it to `cluster.json`.

### On Server C
Use `deploy/cluster_server.json`. Name it `cluster.json`.

## Step 2: Establish Tunnels

### On Laptop A (Nodes 1-5)
```bash
bash deploy/start_tunnel_A.sh
```

### On Laptop B (Nodes 6-10)
```bash
bash deploy/start_tunnel_B.sh
```

## Step 3: Run the Nodes

### Server C (Nodes 11-15)
SSH into `.54`. Ensure `cluster.json` is the **Server** version.
```bash
./shard-server -id 11 -config cluster.json &
./shard-server -id 12 -config cluster.json &
./shard-server -id 13 -config cluster.json &
./shard-server -id 14 -config cluster.json &
./shard-server -id 15 -config cluster.json &
```

### Laptop A (Nodes 1-5)
```bash
./shard-server -id 1 -config cluster.json &
./shard-server -id 2 -config cluster.json &
./shard-server -id 3 -config cluster.json &
./shard-server -id 4 -config cluster.json &
./shard-server -id 5 -config cluster.json &
```

### Laptop B (Nodes 6-10)
```bash
./shard-server -id 6 -config cluster.json &
./shard-server -id 7 -config cluster.json &
./shard-server -id 8 -config cluster.json &
./shard-server -id 9 -config cluster.json &
./shard-server -id 10 -config cluster.json &
```

## Step 4: Verification
- **Server Logs:** Should show successful connections to `127.0.0.1:7xxx`.
