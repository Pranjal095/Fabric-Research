#!/bin/bash
set -e

# Generate sharding.json for the Dependency-Aware Execution Mechanism Raft ShardManagers.
# This file must be placed in the FABRIC_CFG_PATH of the peers so ShardManager can parse it.
# The custom logic maps a Contract Name -> List of Replica IPs.
# We will map all shards to all 7 peers across both servers based on the connection profile.

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
SHARDING_FILE="$DIR/deploy/sharding.json"

if [ -d "$SHARDING_FILE" ]; then
    echo "Docker auto-mounted sharding.json as a directory instead of a file. Cleaning it up..."
    rm -rf "$SHARDING_FILE"
fi

echo "Generating $SHARDING_FILE..."

cat << 'EOF' > "$SHARDING_FILE"
{
    "fabcar": [
        "192.168.50.54:7051",
        "192.168.50.54:8051",
        "192.168.50.54:9051",
        "10.96.1.87:7051",
        "10.96.1.87:8051",
        "10.96.1.87:9051",
        "10.96.1.87:10051"
    ],
    "marbles": [
        "192.168.50.54:7051",
        "192.168.50.54:8051",
        "192.168.50.54:9051",
        "10.96.1.87:7051",
        "10.96.1.87:8051",
        "10.96.1.87:9051",
        "10.96.1.87:10051"
    ],
    "smallbank": [
        "192.168.50.54:7051",
        "192.168.50.54:8051",
        "192.168.50.54:9051",
        "10.96.1.87:7051",
        "10.96.1.87:8051",
        "10.96.1.87:9051",
        "10.96.1.87:10051"
    ],
    "asset-transfer-basic": [
        "192.168.50.54:7051",
        "192.168.50.54:8051",
        "192.168.50.54:9051",
        "10.96.1.87:7051",
        "10.96.1.87:8051",
        "10.96.1.87:9051",
        "10.96.1.87:10051"
    ],
    "token-erc20": [
        "192.168.50.54:7051",
        "192.168.50.54:8051",
        "192.168.50.54:9051",
        "10.96.1.87:7051",
        "10.96.1.87:8051",
        "10.96.1.87:9051",
        "10.96.1.87:10051"
    ],
    "commercial-paper": [
        "192.168.50.54:7051",
        "192.168.50.54:8051",
        "192.168.50.54:9051",
        "10.96.1.87:7051",
        "10.96.1.87:8051",
        "10.96.1.87:9051",
        "10.96.1.87:10051"
    ],
    "auction": [
        "192.168.50.54:7051",
        "192.168.50.54:8051",
        "192.168.50.54:9051",
        "10.96.1.87:7051",
        "10.96.1.87:8051",
        "10.96.1.87:9051",
        "10.96.1.87:10051"
    ]
}
EOF

echo "sharding.json generated successfully. To ensure peers parse it, it will be mapped into their containers via docker-compose."
