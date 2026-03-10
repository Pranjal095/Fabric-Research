#!/bin/bash
set -e

WORKSPACE_DIR="/home/ubuntu/go/src/github.com/hyperledger/fabric/deploy/caliper-workspace"

echo "Step 1: Forcing Local Caliper Installation in $WORKSPACE_DIR"
cd "$WORKSPACE_DIR"

if [ ! -d "node_modules/@hyperledger/caliper-cli" ]; then
    echo "Installing @hyperledger/caliper-cli@0.4.2 locally to guarantee execution path..."
    npm init -y >/dev/null 2>&1 || true
    npm install @hyperledger/caliper-cli@0.4.2 --save-exact
else
    echo "Caliper CLI is already installed locally."
fi

# The core module installed as a dependency
CALIPER_DIR="$WORKSPACE_DIR/node_modules/@hyperledger/caliper-core/lib"

if [ ! -d "$CALIPER_DIR" ]; then
    echo "CRITICAL ERROR: Could not find node_modules/@hyperledger/caliper-core/lib in $WORKSPACE_DIR"
    exit 1
fi

echo "Step 2: Applying P95/P99 Patch to strict local node_modules..."
cd /home/ubuntu/go/src/github.com/hyperledger/fabric/deploy/caliper-patch

cp ./report.js "$CALIPER_DIR/manager/report/"
cp ./default-observer.js "$CALIPER_DIR/manager/test-observers/"
cp ./transaction-statistics-collector.js "$CALIPER_DIR/common/core/"

echo "--------------------------------------------------------"
echo "✅ PATCH SUCCESSFUL! ✅"
echo "--------------------------------------------------------"
echo "CRITICAL: To run Caliper and use this patch, you MUST run it exactly like this:"
echo ""
echo "cd $WORKSPACE_DIR"
echo "npx caliper launch manager \\"
echo "  --caliper-workspace ./ \\"
echo "  --caliper-networkconfig network-config.yaml \\"
echo "  --caliper-benchconfig benchmarks/config_exp1.yaml \\"
echo "  --caliper-flow-only-test"
echo ""
echo "(Note: REMOVE the '--yes @hyperledger/caliper-cli@0.4.2' part from your command, "
echo "as 'npx --yes' forces a temporary download that skips your node_modules!)"
