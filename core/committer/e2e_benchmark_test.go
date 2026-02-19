/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package committer

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	ledger2 "github.com/hyperledger/fabric/core/ledger"
	"github.com/stretchr/testify/mock"
)

// E2EConfig holds parameters for the full architecture simulation
type E2EConfig struct {
	TxCount        int
	DependencyRate float64
	ThreadCount    int
	ClusterSize    int // 1, 3, 5, 7
	Mode           BenchmarkMode
}

// E2EResult holds end-to-end performance metrics
type E2EResult struct {
	Config                E2EConfig
	Throughput            float64       // Tx/sec (Committed / Total Time)
	AvgResponseTime       time.Duration // Client perceived latency (Commit)
	AvgCommitResponseTime time.Duration // Pure Commit phase duration
	RejectRate            float64
	TotalTime             time.Duration
}

// RaftConsensusModel simulates the latency of Raft consensus based on cluster size
// Formula: BaseLatency + (Log(ClusterSize) * NetworkFactor)
func RaftConsensusModel(clusterSize int) time.Duration {
	if clusterSize <= 1 {
		return 0 // No consensus overhead for single node/raft
	}
	// Base latency for network round trip (e.g., 20ms)
	baseLatency := 20 * time.Millisecond
	// Overhead factor per node (network hops, serialization)
	networkFactor := 10 * time.Millisecond

	// Simple logarithmic model: communication complexity grows with log of nodes for Raft leader
	overhead := time.Duration(math.Log2(float64(clusterSize))) * networkFactor
	return baseLatency + overhead
}

// EndorsementThroughputModel simulates the parallel processing capability of SHARDED endorsement.
// If ClusterSize = 1, we process at rate X.
// If ClusterSize = N, we process at rate N*X (linear scaling of endorsement throughput).
// We simulate this by reducing the "Effective Endorsement Delay" per block.
func EndorsementThroughputModel(clusterSize int, txCount int) time.Duration {
	// Base endorsement time per transaction (CPU cost) ~0.5ms
	baseEndorsementTime := 500 * time.Microsecond

	// Total serial work
	totalWork := time.Duration(txCount) * baseEndorsementTime

	// In Original (Non-Sharded), we assume effectively 1 active endorser set per channel often limits throughput
	// or at least we treat Cluster 1 as baseline.
	// In Sharded (Proposed), we have `clusterSize` shards processing in parallel.

	// Effective delay = TotalWork / ClusterSize
	// Note: This is an idealized model where load is perfectly balanced.
	effectiveDelay := totalWork / time.Duration(clusterSize)

	// However, we can't go faster than the Orderer can cut blocks (OrdererBottleneck).
	// Let's assume Orderer is fast but not infinite.
	return effectiveDelay
}

// runE2EBenchmark executes the full flow simulation
func runE2EBenchmark(b *testing.B, config E2EConfig) E2EResult {
	// 1. Setup Committer (The real component we are testing)
	ledger := &mockLedger{
		height:       1,
		currentHash:  []byte("hash"),
		previousHash: []byte("prev"),
	}
	var matchAny = mock.Anything
	ledger.On("CommitLegacy", matchAny).Return(nil)

	committer := NewLedgerCommitter(ledger)
	if config.ThreadCount > 0 {
		committer.SetConcurrencyLimit(config.ThreadCount)
	} else {
		if config.Mode == ModeOriginal {
			committer.SetConcurrencyLimit(1)
		} else {
			committer.SetConcurrencyLimit(runtime.NumCPU())
		}
	}

	// 2. Generate Workload
	block := createBenchmarkBlock(BenchmarkConfig{
		TxCount:        config.TxCount,
		DependencyRate: config.DependencyRate,
		ThreadCount:    config.ThreadCount,
	})

	// 3. Start Timer
	start := time.Now()

	// 4. Sharded Endorsement Simulation
	// Latency Component: Raft verification (increases with cluster size)
	raftLatency := RaftConsensusModel(config.ClusterSize)

	// Throughput Component: Parallel Endorsement
	// We simulate the time it takes for endorsement.
	// Since "Cluster Size" = Nodes per Shard (Replication), increasing it does NOT increase throughput (it might add overhead).
	// We assume a single shard for these experiments to isolate the Raft/Consensus cost.

	// Base endorsement time per transaction (CPU cost) ~0.5ms
	baseEndorsementTime := 500 * time.Microsecond

	// Total serial work (assuming 1 Shard Leader processes them sequentially or limited parallelism)
	// For simulation, we'll assume the bottleneck is the serial ordering at the leader.
	endorsementDelay := time.Duration(config.TxCount) * baseEndorsementTime
	// Divide by some parallelism factor if we assume the leader uses threads, but let's keep it simple:
	// If we use multiple threads for endorsement simulation, we divide.
	// Let's assume the Endorser is parallelized (16 threads).
	endorsementDelay = endorsementDelay / 16

	time.Sleep(raftLatency)
	time.Sleep(endorsementDelay)

	// 5. Ordering Phase Simulation (Fixed)
	time.Sleep(50 * time.Millisecond)

	// 6. Committer Phase (Real Execution)
	var err error
	blockAndPvt := &ledger2.BlockAndPvtData{Block: block}

	if config.Mode == ModeOriginal {
		if err := simulateSerialValidation(block, committer); err != nil {
			fmt.Printf("Serial validation failed: %v\n", err)
		}
	} else {
		err = committer.CommitLegacy(blockAndPvt, &ledger2.CommitOptions{})
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	totalTime := time.Since(start)

	// 7. Calculate Metrics
	throughput := float64(config.TxCount) / totalTime.Seconds()
	avgResponseTime := totalTime

	rejectCount := 0
	if len(block.Metadata.Metadata) > int(common.BlockMetadataIndex_TRANSACTIONS_FILTER) {
		bitmap := block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER]
		for _, code := range bitmap {
			if code != 0 {
				rejectCount++
			}
		}
	}
	rejectRate := float64(rejectCount) / float64(config.TxCount)

	return E2EResult{
		Config:          config,
		Throughput:      throughput,
		AvgResponseTime: avgResponseTime,
		RejectRate:      rejectRate,
		TotalTime:       totalTime,
	}
}

func TestE2EBenchmarkSuite(t *testing.T) {
	rand.Seed(42)
	fmt.Println("Metric,Mode,ClusterSize,TxCount,DepRate,Threads,Value")

	// EXP 1: Throughput/Reject vs Tx Count (Cluster 1, 3, 5)
	fmt.Println("Starting EXP 1: Throughput vs Tx Count (Varying Cluster)")
	txCounts := []int{1000, 2000, 3000, 4000, 5000}
	clusterSizes := []int{1, 3, 5}

	for _, mode := range []BenchmarkMode{ModeOriginal, ModeProposed} {
		for _, cluster := range clusterSizes {
			for _, count := range txCounts {
				cfg := E2EConfig{
					TxCount: count, DependencyRate: 0.4,
					ThreadCount: 32, ClusterSize: cluster, Mode: mode,
				}
				res := runE2EBenchmark(nil, cfg)
				fmt.Printf("Throughput,%s,%d,%d,0.4,32,%.2f\n", mode, cluster, count, res.Throughput)
				fmt.Printf("RejectRate,%s,%d,%d,0.4,32,%.2f\n", mode, cluster, count, res.RejectRate)
				fmt.Printf("ResponseTime,%s,%d,%d,0.4,32,%.4f\n", mode, cluster, count, res.AvgResponseTime.Seconds())
			}
		}
	}

	// EXP 2: Throughput/Reject vs Dependency
	fmt.Println("Starting EXP 2: Throughput vs Dependency")
	deps := []float64{0.0, 0.1, 0.2, 0.3, 0.4, 0.5}
	for _, mode := range []BenchmarkMode{ModeOriginal, ModeProposed} {
		for _, cluster := range []int{1, 3, 5} {
			for _, dep := range deps {
				cfg := E2EConfig{
					TxCount: 1000, DependencyRate: dep,
					ThreadCount: 32, ClusterSize: cluster, Mode: mode,
				}
				res := runE2EBenchmark(nil, cfg)
				fmt.Printf("Throughput,%s,%d,1000,%.1f,32,%.2f\n", mode, cluster, dep, res.Throughput)
				fmt.Printf("RejectRate,%s,%d,1000,%.1f,32,%.2f\n", mode, cluster, dep, res.RejectRate)
			}
		}
	}

	// EXP 3: Throughput vs Threads
	fmt.Println("Starting EXP 3: Throughput vs Threads")
	threads := []int{1, 2, 4, 8, 16, 32}
	for _, cluster := range []int{1, 3, 5} {
		for _, th := range threads {
			cfg := E2EConfig{
				TxCount: 1000, DependencyRate: 0.4,
				ThreadCount: th, ClusterSize: cluster, Mode: ModeProposed,
			}
			res := runE2EBenchmark(nil, cfg)
			fmt.Printf("Throughput,Proposed,%d,1000,0.4,%d,%.2f\n", cluster, th, res.Throughput)
			fmt.Printf("RejectRate,Proposed,%d,1000,0.4,%d,%.2f\n", cluster, th, res.RejectRate)
		}
	}

	// EXP 4: Throughput vs Cluster Size Directly
	fmt.Println("Starting EXP 4: Throughput vs Cluster Size")
	clusters := []int{1, 3, 5, 7}
	for _, mode := range []BenchmarkMode{ModeOriginal, ModeProposed} {
		for _, c := range clusters {
			cfg := E2EConfig{
				TxCount: 1000, DependencyRate: 0.4,
				ThreadCount: 32, ClusterSize: c, Mode: mode,
			}
			res := runE2EBenchmark(nil, cfg)
			fmt.Printf("Throughput,%s,%d,1000,0.4,32,%.2f\n", mode, c, res.Throughput)
			fmt.Printf("ResponseTime,%s,%d,1000,0.4,32,%.4f\n", mode, c, res.AvgResponseTime.Seconds())
		}
	}
}
