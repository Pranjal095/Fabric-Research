#!/bin/bash
cd "$(dirname "$0")"

echo "Building cryptogen and configtxgen..."
cd ..
make cryptogen configtxgen osnadmin
cd deploy

echo "Cleaning up old crypto..."
rm -rf crypto-config mychannel.block

echo "Generating crypto materials for 1 Orderer and 7 Peers..."
../build/bin/cryptogen generate --config=./crypto-config.yaml --output="crypto-config"

echo "Generating Orderer genesis and channel block (mychannel.block) exclusively from ${PWD}/configtx.yaml..."
export FABRIC_CFG_PATH=${PWD}
../build/bin/configtxgen -profile TwoOrgsApplicationGenesis -channelID mychannel -outputBlock ./mychannel.block -configPath ${PWD}

echo "Done! Crypto materials are in deploy/crypto-config/ and mychannel.block is ready."
