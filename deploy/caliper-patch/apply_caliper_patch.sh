#!/bin/bash
set -e

echo "Searching for all Caliper P95/P99 latency patch targets on this machine..."

# Find all instances of report.js in any npx cache
TARGETS=$(find ~/.npm/_npx -type f -name "report.js" -path "*/@hyperledger/caliper-core/*" 2>/dev/null)

if [ -z "$TARGETS" ]; then
    echo "Could not find any @hyperledger/caliper-core installations!"
    echo "Please run your Caliper benchmark command once so it downloads, then run this patch script again."
    exit 1
fi

for REPORT_JS in $TARGETS; do
    CALIPER_DIR=$(dirname $(dirname $(dirname "$REPORT_JS")))
    echo "Patching instance at: $CALIPER_DIR"
    
    cp ./report.js "$CALIPER_DIR/manager/report/"
    cp ./default-observer.js "$CALIPER_DIR/manager/test-observers/"
    cp ./transaction-statistics-collector.js "$CALIPER_DIR/common/core/"
done

echo "All Caliper instances patched successfully! Your next benchmark run WILL have P95/P99."
