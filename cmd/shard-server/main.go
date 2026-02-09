package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/core/endorser/sharding"
)

var logger = flogging.MustGetLogger("shard-server")

// ClusterConfig represents the cluster topology
type ClusterConfig struct {
	Peers map[uint64]string `json:"peers"` // ID -> "IP:Port"
}

func main() {
	var (
		nodeID     uint64
		configFile string
		shardID    string
		txCount    int
	)

	flag.Uint64Var(&nodeID, "id", 0, "Node ID (must be > 0)")
	flag.StringVar(&configFile, "config", "cluster.json", "Path to cluster config file")
	flag.StringVar(&shardID, "shard", "my-shard", "Shard ID/Contract Name")
	flag.IntVar(&txCount, "load", 0, "Number of transactions to generate (0 for follower mode)")
	flag.Parse()

	if nodeID == 0 {
		logger.Error("Node ID must be greater than 0")
		os.Exit(1)
	}

	// Load config
	configData, err := ioutil.ReadFile(configFile)
	if err != nil {
		logger.Errorf("Failed to read config file: %v", err)
		os.Exit(1)
	}

	var clusterConfig ClusterConfig
	if err := json.Unmarshal(configData, &clusterConfig); err != nil {
		logger.Errorf("Failed to parse config file: %v", err)
		os.Exit(1)
	}

	myAddr, ok := clusterConfig.Peers[nodeID]
	if !ok {
		logger.Errorf("Node ID %d not found in config", nodeID)
		os.Exit(1)
	}

	logger.Infof("Starting Shard Node %d at %s", nodeID, myAddr)

	// Generate dummy nodes list for ShardLeader config
	// This relies on the assumption that IDs in cluster.json map to 1..N indices in the Raft peers list
	// if we strictly follow the slice index rule.
	// A robust mapping would be safer but sticking to the existing pattern for now.
	dummyNodes := make([]string, len(clusterConfig.Peers))
	for i := range dummyNodes {
		dummyNodes[i] = fmt.Sprintf("node%d", i+1)
	}

	cfg := sharding.ShardConfig{
		ShardID:      shardID,
		ReplicaNodes: dummyNodes,
		ReplicaID:    nodeID,
	}

	leader, err := sharding.NewShardLeader(cfg, 300*time.Millisecond, 50)
	if err != nil {
		logger.Errorf("Failed to create shard leader: %v", err)
		os.Exit(1)
	}

	// Create Transport
	peerConfig := sharding.PeerConfig(clusterConfig.Peers)
	transport := sharding.NewTransport(nodeID, myAddr, peerConfig, leader)

	if err := transport.Start(); err != nil {
		logger.Errorf("Failed to start transport: %v", err)
		os.Exit(1)
	}

	logger.Info("Shard Server Started Successfully")

	// Run workload if requested
	if txCount > 0 {
		go runWorkload(leader, txCount, shardID, nodeID)
	}

	// Block until signal
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

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
