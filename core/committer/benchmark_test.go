/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package committer

import (
	"fmt"
	"math/rand"
	"runtime"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	ledger2 "github.com/hyperledger/fabric/core/ledger"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/mock"
)

// BenchmarkConfig holds parameters for running a benchmark
type BenchmarkConfig struct {
	TxCount        int
	DependencyRate float64 // 0.0 to 1.0
	ThreadCount    int     // Number of parallel threads (if applicable)
}

// BenchmarkResult holds the results of a benchmark run
type BenchmarkResult struct {
	Throughput     float64       // Tx/sec
	AvgLatency     time.Duration // Average latency per transaction
	TotalTime      time.Duration
	CommittedCount int
	SuccessCount   int
	Config         BenchmarkConfig
}

// createBenchmarkTransaction creates a simplified transaction structure expected by committer_impl.go
func createBenchmarkTransaction(txID, key, value, dependentTxID string) *pb.Transaction {
	// Create chaincode action directly
	// Note: committer_impl.go expects Action.Payload to BE the ChaincodeAction,
	// skipping the ChaincodeActionPayload -> ChaincodeEndorsedAction -> ProposalResponsePayload wrappers
	// which is a deviation from standard Fabric but consistent within this codebase's committer_impl.go
	chaincodeAction := &pb.ChaincodeAction{
		Response: &pb.Response{
			Status:  200,
			Message: fmt.Sprintf("DependencyInfo:HasDependency=%v,DependentTxID=%s", dependentTxID != "", dependentTxID),
		},
		Results: createTestRWSet(key, value),
	}
	chaincodeActionBytes, _ := proto.Marshal(chaincodeAction)

	// Create transaction
	tx := &pb.Transaction{
		Actions: []*pb.TransactionAction{
			{
				Payload: chaincodeActionBytes,
			},
		},
	}

	return tx
}

// createBenchmarkBlock creates a block with a specific number of transactions and dependency pattern
func createBenchmarkBlock(config BenchmarkConfig) *common.Block {
	block := &common.Block{
		Header: &common.BlockHeader{Number: 1},
		Data:   &common.BlockData{Data: make([][]byte, config.TxCount)},
		Metadata: &common.BlockMetadata{
			Metadata: make([][]byte, common.BlockMetadataIndex_TRANSACTIONS_FILTER+1),
		},
	}

	lastDependentTxID := ""

	for i := 0; i < config.TxCount; i++ {
		txID := fmt.Sprintf("tx-%d", i)
		key := fmt.Sprintf("key-%d", i)
		dependentTxID := ""

		if rand.Float64() < config.DependencyRate {
			if lastDependentTxID != "" {
				dependentTxID = lastDependentTxID
			}
			lastDependentTxID = txID
			key = "hot-key"
		}

		// Create Transaction using the simplified structure
		tx := createBenchmarkTransaction(txID, key, "value", dependentTxID)
		txBytes, _ := proto.Marshal(tx)

		// Create ChannelHeader with TxID
		chdr := &common.ChannelHeader{
			TxId: txID,
			Type: int32(common.HeaderType_ENDORSER_TRANSACTION),
		}
		chdrBytes, _ := proto.Marshal(chdr)

		// Create Payload
		payload := &common.Payload{
			Header: &common.Header{
				ChannelHeader: chdrBytes,
			},
			Data: txBytes,
		}
		payloadBytes, _ := proto.Marshal(payload)

		// Create Envelope
		env := &common.Envelope{
			Payload: payloadBytes,
		}
		envBytes, _ := proto.Marshal(env)

		block.Data.Data[i] = envBytes
	}
	return block
}

// runBenchmark executes the benchmark with the given config
func runBenchmark(b *testing.B, config BenchmarkConfig) BenchmarkResult {
	// Setup
	block := createBenchmarkBlock(config)
	// lc := createTestLedgerCommitter(nil) // Pass nil or legitimate testing.T if needed, but here we just need the struct
	// Note: createTestLedgerCommitter in committer_test.go takes *testing.T, we might need to adapt or just copy
	// the logic if we can't export it. For now, assuming we can access it or duplicate it.
	// Duplicate minimal logic here to avoid dependency on test file internals if they are not exported.

	// Create a fresh ledger mock for this run
	ledger := &mockLedger{
		height:       1,
		currentHash:  []byte("test-hash"),
		previousHash: []byte("test-prev-hash"),
	}
	// Mock CommitLegacy to emulate ledger write time (minimal)
	var matchAny = mock.Anything
	ledger.On("CommitLegacy", matchAny).Return(nil)

	committer := NewLedgerCommitter(ledger)

	blockAndPvtData := &ledger2.BlockAndPvtData{
		Block: block,
	}

	// Start timing
	start := time.Now()

	// Execute Commit
	// We use the public CommitLegacy method which triggers the DAG logic
	err := committer.CommitLegacy(blockAndPvtData, &ledger2.CommitOptions{})

	totalTime := time.Since(start)

	if err != nil {
		b.Fatalf("Commit failed: %v", err)
	}

	// Calculate metrics
	throughput := float64(config.TxCount) / totalTime.Seconds()
	avgLatency := totalTime / time.Duration(config.TxCount) // Simplified latency: total batch time / count

	// In a real DAG commit, latency would be measured per-tx from submission,
	// but here we measure the *processing throughput* of the committer.

	return BenchmarkResult{
		Throughput:     throughput,
		AvgLatency:     avgLatency,
		TotalTime:      totalTime,
		CommittedCount: config.TxCount,
		Config:         config,
	}
}

// BenchmarkMode enum
type BenchmarkMode int

const (
	ModeOriginal BenchmarkMode = iota
	ModeProposed
)

func (m BenchmarkMode) String() string {
	if m == ModeOriginal {
		return "Original"
	}
	return "Proposed"
}

// Extended Benchmark Result
type ExtendedBenchmarkResult struct {
	Mode           BenchmarkMode
	TxCount        int
	ThreadCount    int
	DependencyRate float64
	Throughput     float64
	AvgLatency     time.Duration
	RejectRate     float64
	TotalTime      time.Duration
}

// runBenchmarkExtended executes with specific mode
func runBenchmarkExtended(config BenchmarkConfig, mode BenchmarkMode) ExtendedBenchmarkResult {
	// 1. Create Block with potential conflicts
	// To measure reject rate, we need conflicts.
	// We'll generate a block where dependent transactions RE-WRITE the same key.
	// In Original mode (Simulated Serial/Concurrent without DAG), these might conflict if versions overlap?
	// Actually, simulating full MVCC is hard without a real ledger state.
	// But `committer_impl.go` has `checkRWSetConflicts`.
	// We will rely on that detection.

	block := createBenchmarkBlock(config)

	ledger := &mockLedger{
		height:       1,
		currentHash:  []byte("hash"),
		previousHash: []byte("prev"),
	}
	var matchAny = mock.Anything
	ledger.On("CommitLegacy", matchAny).Return(nil)

	committer := NewLedgerCommitter(ledger)
	// Set thread limit based on configuration
	// If config.ThreadCount is <= 0, it means unbounded (or default)
	// For "Original" mode, this setting doesn't matter as we simulate serial execution.
	if config.ThreadCount > 0 {
		committer.SetConcurrencyLimit(config.ThreadCount)
	}

	blockAndPvt := &ledger2.BlockAndPvtData{Block: block}

	start := time.Now()

	// Execute based on mode
	var err error
	if mode == ModeOriginal {
		// Simulate Standard Fabric: Serial Validation
		// We perform strict serial unmarshalling and checking of the entire block once.
		if err := simulateSerialValidation(block, committer); err != nil {
			fmt.Printf("Serial validation failed: %v\n", err)
		}
	} else {
		// Proposed: Use the DAG logic
		err = committer.CommitLegacy(blockAndPvt, &ledger2.CommitOptions{})
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	totalTime := time.Since(start)

	// Parse results for rejection
	rejectCount := 0
	// For Proposed, we check validation flags in block metadata (updated by processBlockWithDAG)
	// For Original simulation, we just count based on our simulated logic.

	// Since we are mocking the ledger, the `txFilter` update in `processBlockWithDAG` happens on the block object.
	// We can inspect `block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER]`

	if len(block.Metadata.Metadata) > int(common.BlockMetadataIndex_TRANSACTIONS_FILTER) {
		bitmap := block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER]
		for _, code := range bitmap {
			if code != uint8(pb.TxValidationCode_VALID) {
				rejectCount++
			}
		}
	}

	throughput := float64(config.TxCount) / totalTime.Seconds()
	avgLatency := totalTime / time.Duration(config.TxCount) // simplified
	rejectRate := float64(rejectCount) / float64(config.TxCount)

	return ExtendedBenchmarkResult{
		Mode:           mode,
		TxCount:        config.TxCount,
		ThreadCount:    config.ThreadCount,
		DependencyRate: config.DependencyRate,
		Throughput:     throughput,
		AvgLatency:     avgLatency,
		RejectRate:     rejectRate,
		TotalTime:      totalTime,
	}
}

func simulateSerialValidation(block *common.Block, lc *LedgerCommitter) error {
	// Simulate standard serial validation: Unmarshal and basic validity checks
	for i := 0; i < len(block.Data.Data); i++ {
		txEnvBytes := block.Data.Data[i]
		if len(txEnvBytes) == 0 {
			continue
		}

		// 1. Unmarshal Envelope
		env, err := protoutil.GetEnvelopeFromBlock(txEnvBytes)
		if err != nil {
			continue
		}

		// 2. Unmarshal Payload
		payload, err := protoutil.UnmarshalPayload(env.Payload)
		if err != nil {
			continue
		}

		// 3. Unmarshal Transaction
		tx, err := protoutil.UnmarshalTransaction(payload.Data)
		if err != nil {
			continue
		}

		// 4. Check Chaincode Actions (similar to parallel logic)
		for _, action := range tx.Actions {
			chaincodeAction := &pb.ChaincodeAction{}
			if err := proto.Unmarshal(action.Payload, chaincodeAction); err != nil {
				continue
			}
			// Access response to match workload
			if chaincodeAction.Response != nil {
				_ = chaincodeAction.Response.Status
			}
		}

		// Simulate VSCC (Signature Verification)
		simulateVSCC()

		// 5. MVCC Check Simulation
		// In a real serial committer, we'd check against the StateDB (Map).
		// We can simulate a map lookup/write cost here if needed,
		// but the unmarshalling is the dominant CPU cost we are optimizing.
	}
	return nil
}

func TestBenchmarkSuite(t *testing.T) {
	rand.Seed(42)
	fmt.Println("Metric,Mode,TxCount,DepRate,Threads,Value")

	// 1. Throughput/Reject vs Tx Count (Comparing Strategies)
	fmt.Println("Starting Experiment 1: Strategy Comparison vs Tx Count")
	txCounts := []int{1000, 2000, 3000, 4000, 5000}

	// Define strategies
	type Strategy struct {
		Name       string
		Mode       BenchmarkMode
		ThreadFunc func() int
	}

	strategies := []Strategy{
		{Name: "Original", Mode: ModeOriginal, ThreadFunc: func() int { return 1 }},
		{Name: "Modified(Dynamic)", Mode: ModeProposed, ThreadFunc: func() int { return runtime.NumCPU() }},
		{Name: "Fixed-2", Mode: ModeProposed, ThreadFunc: func() int { return 2 }},
		{Name: "Fixed-4", Mode: ModeProposed, ThreadFunc: func() int { return 4 }},
	}

	for _, count := range txCounts {
		for _, strat := range strategies {
			threads := strat.ThreadFunc()
			fmt.Printf("Running Strategy=%s Count=%d Threads=%d\n", strat.Name, count, threads)

			cfg := BenchmarkConfig{TxCount: count, DependencyRate: 0.4, ThreadCount: threads}
			res := runBenchmarkExtended(cfg, strat.Mode)

			// Output format: Metric,Strategy,TxCount,DepRate,Threads,Value
			fmt.Printf("Throughput,%s,%d,0.4,%d,%.2f\n", strat.Name, count, threads, res.Throughput)
			fmt.Printf("RejectRate,%s,%d,0.4,%d,%.2f\n", strat.Name, count, threads, res.RejectRate)
		}
	}

	// 2. Throughput/Reject vs Dependency (Focus on Modified/Dynamic Strategy)
	fmt.Println("Starting Experiment 2: Dependency Impact on Dynamic Strategy")
	deps := []float64{0.0, 0.1, 0.2, 0.3, 0.4, 0.5}
	cpuCount := runtime.NumCPU()

	for _, dep := range deps {
		cfg := BenchmarkConfig{TxCount: 1000, DependencyRate: dep, ThreadCount: cpuCount}
		res := runBenchmarkExtended(cfg, ModeProposed)
		fmt.Printf("Throughput,Modified(Dynamic),1000,%.1f,%d,%.2f\n", dep, cpuCount, res.Throughput)
		fmt.Printf("RejectRate,Modified(Dynamic),1000,%.1f,%d,%.2f\n", dep, cpuCount, res.RejectRate)
	}
}
