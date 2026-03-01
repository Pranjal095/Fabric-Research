#!/bin/bash
echo "=== Fabric Network Cleanup ==="
echo "Stopping all containers..."
docker rm -f $(docker ps -a | grep -E "hyperledger/fabric|couchdb|cross_shard" | awk '{print $1}') 2>/dev/null || true

echo "Removing all unused network volumes related to Peer, Orderer, and CouchDB..."
docker volume rm $(docker volume ls -q | grep -E "peer[0-9]+|orderer|couchdb") 2>/dev/null || true

# Explicitly prune dangling volumes
docker volume prune -f

echo "Cleanup complete! Automatically generating fresh crypto materials..."
./generate_crypto.sh
echo "=== Ready! You can now run docker-compose up ==="
