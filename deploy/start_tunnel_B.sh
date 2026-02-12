#!/bin/bash
# Run this on Laptop B (192.168.31.249) running Nodes 6-10

SERVER_USER="cs23btech11048"
SERVER_IP="192.168.50.54"
LOCAL_IP="192.168.31.249"

echo "Starting Reverse SSH Tunnel for Nodes 6-10..."
echo "Server C ($SERVER_IP) -> Tunnel -> Laptop B ($LOCAL_IP)"

# -R [ServerPort]:[LaptopIP]:[LaptopPort]
# We must use LaptopIP because the shard-server binds specifically to that IP, not localhost.

ssh -N -R 7006:$LOCAL_IP:7001 \
       -R 7007:$LOCAL_IP:7002 \
       -R 7008:$LOCAL_IP:7003 \
       -R 7009:$LOCAL_IP:7004 \
       -R 7010:$LOCAL_IP:7005 \
       $SERVER_USER@$SERVER_IP
