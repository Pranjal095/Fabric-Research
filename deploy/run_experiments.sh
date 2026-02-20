#!/bin/bash
# ==============================================================================
# Real-World Experiment Runner for Dependency-Aware Fabric Committer
# ==============================================================================

set -e

# Configuration
PEER_ADDRESS="localhost:7051"
ORDERER_ADDRESS="localhost:7050"
CC_NAMES=("fabcar" "marbles" "smallbank" "asset-transfer-basic" "token-erc20" "commercial-paper" "auction")
THREADS=32
BASE_TX=1000

# Helper to run the benchmark client
run_benchmark() {
    local tx_count=$1
    local dependency=$2
    local threads=$3
    local cluster=$4
    local exp_name=$5

    echo "=========================================================="
    echo "Running Experiment: ${exp_name}"
    echo "Txs: ${tx_count} | Dep: ${dependency} | Threads: ${threads} | Cluster: ${cluster}"
    echo "=========================================================="

    # Assuming the benchmark_client uses these flags
    # We pass the real chaincode names to represent the distinct shards
    local shards_arg=$(IFS=, ; echo "${CC_NAMES[*]}")

    # Launch actual benchmark (we use the Go binary)
    ./benchmark_client \
        --peer "${PEER_ADDRESS}" \
        --orderer "${ORDERER_ADDRESS}" \
        --txs "${tx_count}" \
        --dependency "${dependency}" \
        --threads "${threads}" \
        --shards "${shards_arg}" \
        | tee -a "results_${exp_name}.log"

    echo "Completed."
    echo ""
}

# ------------------------------------------------------------------------------
# EXPERIMENT 1: Throughput and Reject Rate vs Tx Count
# ------------------------------------------------------------------------------
# Variables: Txs: 1000-5000, Dep 40%, Threads 32, Cluster 3/5
run_exp1() {
    local cluster_size=$1
    for tx in 1000 2000 3000 4000 5000; do
        run_benchmark $tx 0.40 32 $cluster_size "EXP1_Cluster${cluster_size}_Tx${tx}"
    done
}

# ------------------------------------------------------------------------------
# EXPERIMENT 2: Throughput and Reject Rate vs Dependency
# ------------------------------------------------------------------------------
# Variables: Dep 0-50%, Txs 1000, Threads 32, Cluster 1
run_exp2() {
    local cluster_size=1
    for dep in 0.00 0.10 0.20 0.30 0.40 0.50; do
        run_benchmark 1000 $dep 32 $cluster_size "EXP2_Dep${dep}"
    done
}

# ------------------------------------------------------------------------------
# EXPERIMENT 3: Throughput and Reject Rate vs Threads
# ------------------------------------------------------------------------------
# Variables: Threads: 1, 2, 4, 8, 16, 32, Txs 1000, Dep 40%, Cluster 3
run_exp3() {
    local cluster_size=3
    for th in 1 2 4 8 16 32; do
        run_benchmark 1000 0.40 $th $cluster_size "EXP3_Threads${th}"
    done
}

# ------------------------------------------------------------------------------
# EXPERIMENT 4: Throughput and Reject Rate vs Cluster Size
# ------------------------------------------------------------------------------
# Variables: Cluster 1, 3, 5, 7, Txs 1000, Dep 40%, Threads 32
run_exp4() {
    for c in 1 3 5 7; do
        run_benchmark 1000 0.40 32 $c "EXP4_Cluster${c}"
    done
}

# ------------------------------------------------------------------------------
# EXPERIMENT 5: Response Time vs Transactions Without Retries
# ------------------------------------------------------------------------------
# Same as Exp 1 basically, but we need the output latency breakdowns which the client should print.
run_exp5() {
    local cluster_size=3
    for tx in 1000 2000 3000 4000 5000; do
        run_benchmark $tx 0.40 32 $cluster_size "EXP5_Latency_Tx${tx}"
    done
}

# Main Execution Trigger
echo "Starting Full Evaluation Suite..."
mkdir -p results

# NOTE: In a fully automated real-world test, you would tear down and bring up 
# different docker-compose clusters between tests (especially for Exp 4). 
# For this script, we assume the operator manually configures the cluster topology 
# via sharding.json before kicking off the relevant suite method.

# run_exp1 3
# run_exp1 5
# run_exp2
# run_exp3
# run_exp4
# run_exp5

echo "Please uncomment the specific experiment blocks in the script once the physical cluster matches the requested size."
