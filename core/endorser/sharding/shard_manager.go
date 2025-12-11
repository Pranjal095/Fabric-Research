/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package sharding

import (
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

	if shard, exists := sm.shards[contractName]; exists {
		return shard, nil
	}

	config := ShardConfig{
		ShardID:      contractName,
		ReplicaNodes: []string{"localhost:7051", "localhost:7052", "localhost:7053"},
		ReplicaID:    1,
	}

	shard, err := NewShardLeader(config, DefaultBatchTimeout, DefaultBatchMaxSize)
	if err != nil {
		return nil, err
	}

	sm.shards[contractName] = shard
	logger.Infof("Dynamically created shard for contract %s", contractName)
	return shard, nil
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
