#!/bin/bash
# Run this on Laptop A (192.168.31.179) running Nodes 1-5

SERVER_USER="cs23btech11048"
SERVER_IP="192.168.50.54"
LOCAL_IP="192.168.31.178"

echo "Starting Reverse SSH Tunnel for Nodes 1-5..."
echo "Server C ($SERVER_IP) -> Tunnel -> Laptop A ($LOCAL_IP)"

# -R [ServerPort]:[LaptopIP]:[LaptopPort]
ssh -N -R 7001:$LOCAL_IP:7001 \
       -R 7002:$LOCAL_IP:7002 \
       -R 7003:$LOCAL_IP:7003 \
       -R 7004:$LOCAL_IP:7004 \
       -R 7005:$LOCAL_IP:7005 \
       $SERVER_USER@$SERVER_IP
