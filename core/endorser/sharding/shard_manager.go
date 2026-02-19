/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package sharding

import (
	"encoding/json"
	"os"
	"sync"
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
		ReplicaID:    1,
	}

	// Try to load from configuration file
	// We look for 'sharding.json' in the current directory or config path
	// Structure: {"fabcar": ["100.x.x.2:7051", "100.x.x.2:8051", "100.x.x.2:9051"]}
	if externalConfig, err := loadShardingConfig("sharding.json"); err == nil {
		if replicas, ok := externalConfig[contractName]; ok {
			config.ReplicaNodes = replicas
			logger.Infof("Loaded configuration for shard %s: %v", contractName, replicas)

			// Determine our ID based on CORE_PEER_ADDRESS
			// If we are one of the replicas, we need a unique ID (index + 1)
			myAddr := os.Getenv("CORE_PEER_ADDRESS")
			if myAddr == "" {
				// Fallback or just assume first if testing locally without env
				myAddr = "localhost:7051"
			}

			for i, nodeAddr := range replicas {
				if nodeAddr == myAddr {
					config.ReplicaID = uint64(i + 1)
					break
				}
			}
		}
	}

	shard, err := NewShardLeader(config, DefaultBatchTimeout, DefaultBatchMaxSize)
	if err != nil {
		return nil, err
	}

	sm.shards[contractName] = shard
	logger.Infof("Created shard for contract %s with ReplicaID %d", contractName, config.ReplicaID)
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

// Shutdown stops all shards
func (sm *ShardManager) Shutdown() {
	sm.shardsLock.Lock()
	defer sm.shardsLock.Unlock()

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
		metrics[shardID] = shard.GetRequestsHandled()
	}

	return metrics
}
