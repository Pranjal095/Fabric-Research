#!/bin/bash
# Run this on Laptop B (192.168.31.249) running Nodes 6-10

SERVER_USER="cs23btech11048"
SERVER_IP="192.168.50.54"

echo "Starting Reverse SSH Tunnel for Nodes 6-10..."
echo "Server C ($SERVER_IP):7006-7010 -> Tunnel -> Laptop B:7001-7005"

# -R [ServerPort]:localhost:[LaptopPort]
# Using localhost here because the nodes on Laptop B will now bind to localhost
# thanks to the updated cluster_laptops.json.

ssh -N -R 7006:localhost:7001 \
       -R 7007:localhost:7002 \
       -R 7008:localhost:7003 \
       -R 7009:localhost:7004 \
       -R 7010:localhost:7005 \
       $SERVER_USER@$SERVER_IP
