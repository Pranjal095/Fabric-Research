#!/bin/bash
# Run this on Laptop A (192.168.31.179) running Nodes 1-5

SERVER_USER="cs23btech11048"
SERVER_IP="192.168.50.54"

echo "Starting Reverse SSH Tunnel for Nodes 1-5..."
echo "Server C ($SERVER_IP):7001-7005 -> Tunnel -> Laptop A:7001-7005"

# -R [ServerPort]:localhost:[LaptopPort]
ssh -N -R 7001:localhost:7001 \
       -R 7002:localhost:7002 \
       -R 7003:localhost:7003 \
       -R 7004:localhost:7004 \
       -R 7005:localhost:7005 \
       $SERVER_USER@$SERVER_IP
