#!/bin/bash
set -e

echo "=== Fabric Sharded Network Setup ==="

START_INDEX=${1:-0}

export FABRIC_CFG_PATH=$PWD/../sampleconfig
export CORE_PEER_TLS_ENABLED=true
export CORE_PEER_TLS_ROOTCERT_FILE=$PWD/crypto-config/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt
export CORE_PEER_LOCALMSPID="Org1MSP"
export CORE_PEER_MSPCONFIGPATH=$PWD/crypto-config/peerOrganizations/org1.example.com/users/Admin@org1.example.com/msp

ORDERER_TLS_CA=$PWD/crypto-config/ordererOrganizations/example.com/orderers/orderer.example.com/tls/ca.crt

# Find all running peers locally (handles both 0.0.0.0 and specific IP bindings, with or without port ranges)
PEER_PORTS=$(docker ps | grep "hyperledger/fabric-peer" | grep -oP '[0-9\.]+:\K([0-9]+)(?=(-[0-9]+)?->)' | sort -n -u)

# Check if we are on Server 1 (where the global starting index is 0)
IS_SERVER_1=false
if [ "$START_INDEX" -eq 0 ]; then
    IS_SERVER_1=true
fi

echo "Found local peers on ports:"
echo "$PEER_PORTS"

# We will map ports dynamically to their global peer ID index: INDEX=$(( START_INDEX + ($PORT - 7051) / 1000 ))

if [ "$IS_SERVER_1" = true ]; then
    echo "--- Server 1 Detected: Creating Channel on Orderer ---"
    ../build/bin/osnadmin channel join --channelID mychannel --config-block ./mychannel.block -o 127.0.0.1:7053 --ca-file ./crypto-config/ordererOrganizations/example.com/orderers/orderer.example.com/tls/ca.crt --client-cert ./crypto-config/ordererOrganizations/example.com/users/Admin@example.com/tls/client.crt --client-key ./crypto-config/ordererOrganizations/example.com/users/Admin@example.com/tls/client.key || true
    sleep 2
fi

for PORT in $PEER_PORTS; do
    INDEX=$(( START_INDEX + (($PORT - 7051) / 1000) ))
    echo "--- Joining Peer on Port $PORT (peer${INDEX}) to mychannel ---"
    export CORE_PEER_ADDRESS=localhost:$PORT
    export CORE_PEER_TLS_SERVERHOSTOVERRIDE=peer${INDEX}.org1.example.com
    export CORE_PEER_TLS_ROOTCERT_FILE=$PWD/crypto-config/peerOrganizations/org1.example.com/peers/peer${INDEX}.org1.example.com/tls/ca.crt
    ../build/bin/peer channel join -b mychannel.block -o 192.168.50.54:7050 --ordererTLSHostnameOverride orderer.example.com --tls --cafile $ORDERER_TLS_CA || true
done

echo ""
echo "=== Packaging Cross-Shard Chaincode ==="
cd chaincode/cross_shard
go mod tidy
go mod vendor
cd ../..
../build/bin/peer lifecycle chaincode package cross_shard.tar.gz --path ./chaincode/cross_shard --lang golang --label cross_shard_1.0

for PORT in $PEER_PORTS; do
    INDEX=$(( START_INDEX + (($PORT - 7051) / 1000) ))
    echo "--- Installing Chaincode on Peer on Port $PORT (peer${INDEX}) ---"
    export CORE_PEER_ADDRESS=localhost:$PORT
    export CORE_PEER_TLS_SERVERHOSTOVERRIDE=peer${INDEX}.org1.example.com
    export CORE_PEER_TLS_ROOTCERT_FILE=$PWD/crypto-config/peerOrganizations/org1.example.com/peers/peer${INDEX}.org1.example.com/tls/ca.crt
    ../build/bin/peer lifecycle chaincode install cross_shard.tar.gz || true
done

if [ "$IS_SERVER_1" = true ]; then
    echo "=== Server 1 Detected: Executing Channel Approvals and Commits ==="
    export CORE_PEER_ADDRESS=localhost:7051
    export CORE_PEER_TLS_SERVERHOSTOVERRIDE=peer${START_INDEX}.org1.example.com
    export CORE_PEER_TLS_ROOTCERT_FILE=$PWD/crypto-config/peerOrganizations/org1.example.com/peers/peer${START_INDEX}.org1.example.com/tls/ca.crt
    sleep 2
    CC_PACKAGE_ID=$(../build/bin/peer lifecycle chaincode queryinstalled | grep "cross_shard_1.0" | grep -o 'cross_shard_1.0:[a-f0-9]*' | head -n 1)

    if [ -z "$CC_PACKAGE_ID" ]; then
        echo "Failed to extract accurate CC_PACKAGE_ID! Aborting."
        exit 1
    fi

    echo "Extracted Package ID: $CC_PACKAGE_ID"

    SHARDS=("fabcar" "marbles" "smallbank" "asset-transfer-basic" "token-erc20" "commercial-paper" "auction")

    for CC_NAME in "${SHARDS[@]}"; do
        echo "--- Approving $CC_NAME ---"
        ../build/bin/peer lifecycle chaincode approveformyorg -o 192.168.50.54:7050 \
            --ordererTLSHostnameOverride orderer.example.com --tls --cafile $ORDERER_TLS_CA \
            --channelID mychannel --name ${CC_NAME} --version 1.0 \
            --package-id $CC_PACKAGE_ID --sequence 1

        sleep 5

        echo "--- Committing $CC_NAME ---"
        ../build/bin/peer lifecycle chaincode commit -o 192.168.50.54:7050 \
            --ordererTLSHostnameOverride orderer.example.com --tls --cafile $ORDERER_TLS_CA \
            --channelID mychannel --name ${CC_NAME} --version 1.0 \
            --sequence 1 --peerAddresses localhost:7051 \
            --tlsRootCertFiles $CORE_PEER_TLS_ROOTCERT_FILE
            
        sleep 5
    done
    echo "=== All 7 Chaincode Shards Successfully Deployed & Committed! ==="
else
    echo "=== VM Node Detected: Installation Complete ==="
    echo "You must naturally ensure that you have run this script on the Host Server as well to officially commit the chaincodes to the channel."
fi
