#!/bin/bash
set -e

echo "Applying Caliper P95/P99 latency patch to local npx cache..."

# Find the Caliper-core directory in the active npx cache
# It's usually under ~/.npm/_npx/.../node_modules/@hyperledger/caliper-core

CALIPER_DIR=$(find ~/.npm/_npx -type d -path "*/node_modules/@hyperledger/caliper-core" 2>/dev/null | head -n 1)

if [ -z "$CALIPER_DIR" ]; then
    echo "Could not find @hyperledger/caliper-core in ~/.npm/_npx. Have you run caliper on this machine yet?"
    echo "Please run your Caliper benchmark command once so it downloads, then run this patch script again."
    exit 1
fi

echo "Found Caliper installation at: $CALIPER_DIR"

cp ./report.js "$CALIPER_DIR/lib/manager/report/"
cp ./default-observer.js "$CALIPER_DIR/lib/manager/test-observers/"
cp ./transaction-statistics-collector.js "$CALIPER_DIR/lib/common/core/"

echo "Patch applied successfully!"
