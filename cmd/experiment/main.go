package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/core/endorser/sharding"
)

var logger = flogging.MustGetLogger("experiment.runner")

func main() {
	// Parse CLI arguments
	nodeID := flag.Uint64("id", 0, "Node ID (must be > 0)")
	address := flag.String("address", "", "Address to listen on (host:port)")
	peersStr := flag.String("peers", "", "Comma-separated list of peer addresses (e.g. host1:port1,host2:port2)")
	shardID := flag.String("shard", "experiment-shard", "Shard ID")
	txCount := flag.Int("load", 0, "Number of transactions to generate (0 for follower mode)")
	flag.Parse()

	if *nodeID == 0 || *address == "" || *peersStr == "" {
		fmt.Println("Usage: experiment -id <ID> -address <HOST:PORT> -peers <P1,P2,P3> [-load <TX_COUNT>]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	peers := strings.Split(*peersStr, ",")

	// Create Shard Config
	// Note: ShardLeader assumes peers are ID 1..N based on this slice order
	shardConfig := sharding.ShardConfig{
		ShardID:      *shardID,
		ReplicaNodes: peers,
		ReplicaID:    *nodeID,
	}

	logger.Infof("Starting Node %d at %s for shard %s", *nodeID, *address, *shardID)
	logger.Infof("Peers: %v", peers)

	// Initialize Shard Leader
	// Using slightly larger timeout for real network latency
	leader, err := sharding.NewShardLeader(shardConfig, 500*time.Millisecond, 50)
	if err != nil {
		logger.Fatalf("Failed to create shard leader: %v", err)
	}

	// Create Transport Peer Config map
	peerConfig := make(sharding.PeerConfig)
	for i, peerAddr := range peers {
		id := uint64(i + 1)
		// Assuming peers provided are reachable addresses
		peerConfig[id] = peerAddr
	}

	// Initialize Transport
	transport := sharding.NewTransport(*nodeID, *address, peerConfig, leader)
	if err := transport.Start(); err != nil {
		logger.Fatalf("Failed to start transport: %v", err)
	}

	// Handle graceful shutdown
	stopC := make(chan os.Signal, 1)
	signal.Notify(stopC, syscall.SIGINT, syscall.SIGTERM)

	// Run workload if requested
	if *txCount > 0 {
		go runWorkload(leader, *txCount, *shardID, *nodeID)
	}

	<-stopC
	logger.Info("Shutting down...")
	transport.Stop()
	leader.Stop()
}

func runWorkload(leader *sharding.ShardLeader, count int, shardID string, nodeID uint64) {
	// Wait a bit for leader election to settle
	logger.Info("Waiting 5s for leader election before starting workload...")
	time.Sleep(5 * time.Second)

	logger.Infof("Starting workload: %d transactions", count)
	startTime := time.Now()

	// var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// Monitor commits
	go func() {
		for range leader.CommitC() {
			mu.Lock()
			successCount++
			current := successCount
			mu.Unlock()

			if current%100 == 0 {
				logger.Infof("Progress: %d/%d committed", current, count)
			}
		}
	}()

	// Generate load
	for i := 0; i < count; i++ {
		req := &sharding.PrepareRequest{
			TxID:      fmt.Sprintf("tx-%d-%d-%d", nodeID, time.Now().UnixNano(), i),
			ShardID:   shardID,
			WriteSet:  map[string][]byte{"key": []byte(fmt.Sprintf("val-%d", i))},
			Timestamp: time.Now(),
		}

		select {
		case leader.ProposeC() <- req:
			// Sent
		case <-time.After(1 * time.Second):
			logger.Warnf("Queue full, dropping tx %s", req.TxID)
		}

		// Rate limit slightly
		time.Sleep(1 * time.Millisecond)
	}

	// Wait for completion (simple timeout based)
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(1 * time.Second)

	for {
		select {
		case <-timeout:
			logger.Warn("Workload timed out waiting for all commits")
			return
		case <-ticker.C:
			mu.Lock()
			sc := successCount
			mu.Unlock()
			if sc >= count {
				elapsed := time.Since(startTime)
				tps := float64(sc) / elapsed.Seconds()
				logger.Infof("Workload completed! Throughput: %.2f TPS", tps)
				return
			}
		}
	}
}
