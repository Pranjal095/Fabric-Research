#!/bin/bash
# ==============================================================================
# Real-World Experiment Runner for Fabric Committer with Analytics
# ==============================================================================

set -e

# Configuration
PEER_ADDRESS="localhost:7051"
ORDERER_ADDRESS="localhost:7050"
CC_NAMES=("fabcar" "marbles" "smallbank" "asset-transfer-basic" "token-erc20" "commercial-paper" "auction")

if [ -z "$1" ]; then
    echo "Usage: ./run_experiments.sh <fabric_version>"
    echo "fabric_version: original | proposed | proposed-c1"
    exit 1
fi

FABRIC_VERSION=$1

# Helper to run the benchmark client
run_benchmark() {
    local tx_count=$1
    local dependency=$2
    local threads=$3
    local cluster=$4
    local exp_name=$5

    echo "=========================================================="
    echo "Running Experiment: ${exp_name} | Version: ${FABRIC_VERSION}"
    echo "Txs: ${tx_count} | Dep: ${dependency} | Threads: ${threads} | Cluster: ${cluster}"
    echo "=========================================================="

    local shards_arg=$(IFS=, ; echo "${CC_NAMES[*]}")
    local log_file="results_${exp_name}_${FABRIC_VERSION}.log"

    ./benchmark_client \
        --peer "${PEER_ADDRESS}" \
        --orderer "${ORDERER_ADDRESS}" \
        --txs "${tx_count}" \
        --dependency "${dependency}" \
        --threads "${threads}" \
        --shards "${shards_arg}" \
        | tee "${log_file}"

    echo "Pushing metrics to CouchDB Analytics backend..."
    python3 analytics/upload_results.py "${log_file}" "${FABRIC_VERSION}"
    echo "Completed."
    echo ""
}

# ------------------------------------------------------------------------------
# EXPERIMENT 1: Throughput and Reject Rate vs Tx Count
# ------------------------------------------------------------------------------
run_exp1() {
    local cluster_size=$1 # e.g., 5 or 3
    for tx in 1000 2000 3000 4000 5000; do
        run_benchmark $tx 0.40 32 $cluster_size "EXP1_Cluster${cluster_size}_Tx${tx}"
    done
}

# ------------------------------------------------------------------------------
# EXPERIMENT 2: Throughput and Reject Rate vs Dependency
# ------------------------------------------------------------------------------
run_exp2() {
    local cluster_size=$1 # e.g., 5 or 3
    for dep in 0.00 0.10 0.20 0.30 0.40 0.50; do
        run_benchmark 1000 $dep 32 $cluster_size "EXP2_Dep${dep}"
    done
}

# ------------------------------------------------------------------------------
# EXPERIMENT 3: Throughput and Reject Rate vs Threads
# ------------------------------------------------------------------------------
run_exp3() {
    local cluster_size=$1
    for th in 1 2 4 8 16 32; do
        run_benchmark 1000 0.40 $th $cluster_size "EXP3_Threads${th}"
    done
}

# ------------------------------------------------------------------------------
# EXPERIMENT 4: Throughput and Reject Rate vs Cluster Size
# ------------------------------------------------------------------------------
run_exp4() {
    for c in 1 3 5 7; do
        run_benchmark 1000 0.40 32 $c "EXP4_Cluster${c}"
    done
}

# ------------------------------------------------------------------------------
# EXPERIMENT 5: Response Time vs Transactions Without Retries
# ------------------------------------------------------------------------------
run_exp5() {
    local cluster_size=$1
    for tx in 1000 2000 3000 4000 5000; do
        run_benchmark $tx 0.40 32 $cluster_size "EXP5_Latency_Tx${tx}"
    done
}

echo "Starting Full Evaluation Suite for ${FABRIC_VERSION}..."
mkdir -p results

# NOTE: Adjust cluster sizes below to match the active clustered environment
# Defaulting to cluster size = 3 for general runs (Cluster size 1 for proposed-c1)

CLUSTER_SIZE=3
if [ "${FABRIC_VERSION}" == "proposed-c1" ]; then
    CLUSTER_SIZE=1
fi

# Uncomment the experiments below you wish to perform
# run_exp1 $CLUSTER_SIZE
# run_exp2 $CLUSTER_SIZE
# run_exp3 $CLUSTER_SIZE
# run_exp4
# run_exp5 $CLUSTER_SIZE

echo "Please uncomment the specific experiment loops inside run_experiments.sh."
