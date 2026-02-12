#!/bin/bash
# Run this on Laptop B (192.168.31.249) running Nodes 6-10

SERVER_USER="cs23btech11048"
SERVER_IP="192.168.50.54"

echo "Starting Reverse SSH Tunnel for Nodes 6-10..."
echo "Server C ($SERVER_IP):7006-7010 -> Tunnel -> Laptop B:7001-7005"

# -R [ServerPort]:127.0.0.1:[LaptopPort]
# explicit IPv4 "127.0.0.1" to avoid potential IPv6 "::1" resolution issues with "localhost"

ssh -N -R 7006:127.0.0.1:7001 \
       -R 7007:127.0.0.1:7002 \
       -R 7008:127.0.0.1:7003 \
       -R 7009:127.0.0.1:7004 \
       -R 7010:127.0.0.1:7005 \
       $SERVER_USER@$SERVER_IP
