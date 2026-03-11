/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package sharding

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
)

// Global Transport instance across all ShardManagers in the Peer
var (
	globalTransport     *Transport
	globalTransportLock sync.Mutex
)

// Metrics interface for shard metrics
type Metrics interface{}

// ShardManager manages multiple contract shards
type ShardManager struct {
	shards     map[string]*ShardLeader
	shardsLock sync.RWMutex
	config     map[string]ShardConfig
	metrics    Metrics
}

// NewShardManager creates a shard manager
func NewShardManager(configs map[string]ShardConfig, metrics Metrics) *ShardManager {
	if configs == nil {
		configs = make(map[string]ShardConfig)
	}

	sm := &ShardManager{
		shards:  make(map[string]*ShardLeader),
		config:  configs,
		metrics: metrics,
	}

	// 1. Determine local address for the transport binding
	myAddr := os.Getenv("CORE_PEER_ADDRESS")
	if myAddr == "" {
		myAddr = "localhost:7051"
	}

	// 2. Discover the global replica node list and Initialize Transport
	sm.initGlobalTransportOnce(myAddr)
	if !globalHTTPStarted {
		sm.StartHTTPServer(myAddr)
		globalHTTPStarted = true
	}
	globalTransportLock.Unlock()

	// 6. Pre-initialize any configured shards
	for shardID, config := range configs {
		shard, err := NewShardLeader(config, DefaultBatchTimeout, DefaultBatchMaxSize)
		if err != nil {
			logger.Errorf("Failed to create shard %s: %v", shardID, err)
			continue
		}
		sm.shards[shardID] = shard
		logger.Infof("Initialized shard %s with %d replicas", shardID, len(config.ReplicaNodes))
	}

	return sm
}

// GetOrCreateShard gets or creates a shard for a contract
func (sm *ShardManager) GetOrCreateShard(contractName string) (*ShardLeader, error) {
	sm.shardsLock.RLock()
	shard, exists := sm.shards[contractName]
	sm.shardsLock.RUnlock()

	if exists {
		return shard, nil
	}

	sm.shardsLock.Lock()
	defer sm.shardsLock.Unlock()

	// Double check
	if shard, exists := sm.shards[contractName]; exists {
		return shard, nil
	}

	// Default config
	config := ShardConfig{
		ShardID:      contractName,
		ReplicaNodes: []string{"localhost:7051", "localhost:7052", "localhost:7053"},
		ReplicaIDs:   []uint64{1, 2, 3},
		ReplicaID:    1,
	}

	myAddr := os.Getenv("CORE_PEER_ADDRESS")
	if myAddr == "" {
		myAddr = "localhost:7051"
	}

	// Try to load from configuration file
	if externalConfig, err := loadShardingConfig("sharding.json"); err == nil {
		if replicas, ok := externalConfig[contractName]; ok {
			config.ReplicaNodes = replicas
			logger.Infof("Loaded configuration for shard %s: %v", contractName, replicas)

			// Compute global deterministic mapping
			var globalReplicas []string
			replicaSet := make(map[string]bool)
			for _, repls := range externalConfig {
				for _, r := range repls {
					if !replicaSet[r] {
						replicaSet[r] = true
						globalReplicas = append(globalReplicas, r)
					}
				}
			}
			sort.Strings(globalReplicas)

			// Resolve local replicas directly to global ID map indices
			var globalIDs []uint64
			for _, nodeAddr := range replicas {
				for i, gAddr := range globalReplicas {
					if nodeAddr == gAddr {
						globalIDs = append(globalIDs, uint64(i+1))
						if nodeAddr == myAddr {
							config.ReplicaID = uint64(i + 1)
						}
						break
					}
				}
			}
			config.ReplicaIDs = globalIDs
		}
	}

	shard, err := NewShardLeader(config, DefaultBatchTimeout, DefaultBatchMaxSize)
	if err != nil {
		return nil, err
	}

	sm.initGlobalTransportOnce(myAddr)

	globalTransport.RegisterShard(contractName, shard)

	sm.shards[contractName] = shard

	logger.Infof("Created shard for contract %s with ReplicaID %d and hooked into multiplexed transport", contractName, config.ReplicaID)
	return shard, nil
}

// Helper to load config
func loadShardingConfig(path string) (map[string][]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var config map[string][]string
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return nil, err
	}
	return config, nil
}

func (sm *ShardManager) initGlobalTransportOnce(myAddr string) {
	globalTransportLock.Lock()
	defer globalTransportLock.Unlock()

	if globalTransport != nil {
		return
	}

	var globalReplicas []string
	if externalConfig, err := loadShardingConfig("sharding.json"); err == nil {
		replicaSet := make(map[string]bool)
		for _, replicas := range externalConfig {
			for _, r := range replicas {
				if !replicaSet[r] {
					replicaSet[r] = true
					globalReplicas = append(globalReplicas, r)
				}
			}
		}
		sort.Strings(globalReplicas)
	} else {
		globalReplicas = []string{"localhost:7051", "localhost:7052", "localhost:7053"}
	}

	var replicaID uint64 = 1
	for i, nodeAddr := range globalReplicas {
		if nodeAddr == myAddr {
			replicaID = uint64(i + 1)
			break
		}
	}

	peers := make(PeerConfig)
	for i, addr := range globalReplicas {
		peers[uint64(i+1)] = addr
	}

	transport := NewTransport(replicaID, myAddr, peers)
	if err := transport.Start(); err != nil {
		logger.Errorf("Failed to start global shard transport: %v", err)
	} else {
		globalTransport = transport
		logger.Infof("Started global process-level gRPC transport for ShardManager at %s (ReplicaID: %d)", myAddr, replicaID)
	}
}

// Shutdown stops all shards
func (sm *ShardManager) Shutdown() {
	sm.shardsLock.Lock()
	defer sm.shardsLock.Unlock()

	globalTransportLock.Lock()
	if globalTransport != nil {
		logger.Infof("Stopping global shard transport")
		globalTransport.Stop()
		globalTransport = nil
	}
	globalTransportLock.Unlock()

	for shardID, shard := range sm.shards {
		logger.Infof("Stopping shard %s", shardID)
		shard.Stop()
	}
}

// GetShardMetrics returns metrics for all shards
func (sm *ShardManager) GetShardMetrics() map[string]int64 {
	sm.shardsLock.RLock()
	defer sm.shardsLock.RUnlock()

	metrics := make(map[string]int64)
	for shardID, shard := range sm.shards {
		metrics[shardID] = int64(shard.GetRequestsHandled())
	}

	return metrics
}
