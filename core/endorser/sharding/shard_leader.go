/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package sharding

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hyperledger/fabric/common/flogging"
	"go.etcd.io/etcd/raft/v3"
	"go.etcd.io/etcd/raft/v3/raftpb"
)

var logger = flogging.MustGetLogger("endorser.sharding")

const (
	DefaultBatchMaxSize   = 20
	DefaultBatchTimeout   = 300 * time.Millisecond
	DefaultExpiryDuration = 5 * time.Minute
)

// TransactionDependencyInfo represents information about a transaction dependency
type TransactionDependencyInfo struct {
	Value         []byte
	DependentTxID string
	ExpiryTime    time.Time
	HasDependency bool
}

// ShardConfig represents configuration for a contract shard
type ShardConfig struct {
	ShardID      string
	ReplicaNodes []string
	ReplicaID    uint64
}

// PrepareRequest represents a dependency preparation request
type PrepareRequest struct {
	TxID      string
	ShardID   string
	ReadSet   map[string][]byte
	WriteSet  map[string][]byte
	Timestamp time.Time
}

// PrepareProof represents a committed dependency entry
type PrepareProof struct {
	TxID        string
	ShardID     string
	CommitIndex uint64
	LeaderID    uint64
	Signature   []byte
	Term        uint64
}

// ShardLeader manages a Raft group for a specific contract
type ShardLeader struct {
	shardID         string
	node            raft.Node
	storage         *raft.MemoryStorage
	peers           []raft.Peer
	commitIndex     uint64
	variableMap     map[string]TransactionDependencyInfo
	variableMapLock sync.RWMutex
	batchQueue      []*PrepareRequest
	batchLock       sync.Mutex
	batchTimeout    time.Duration
	maxBatchSize    int
	lastBatchTime   time.Time
	proposeC        chan *PrepareRequest
	commitC         chan *PrepareProof
	errorC          chan error
	stopC           chan struct{}
	messagesC       chan []raftpb.Message
	requestsHandled int64
	mu              sync.RWMutex
}

// NewShardLeader creates a new Raft-based shard leader
func NewShardLeader(config ShardConfig, batchTimeout time.Duration, maxBatchSize int) (*ShardLeader, error) {
	storage := raft.NewMemoryStorage()

	c := &raft.Config{
		ID:              config.ReplicaID,
		ElectionTick:    10,
		HeartbeatTick:   1,
		Storage:         storage,
		MaxSizePerMsg:   1024 * 1024,
		MaxInflightMsgs: 256,
	}

	var peers []raft.Peer
	for i := range config.ReplicaNodes {
		peers = append(peers, raft.Peer{ID: uint64(i + 1)})
	}

	node := raft.StartNode(c, peers)

	sl := &ShardLeader{
		shardID:       config.ShardID,
		node:          node,
		storage:       storage,
		peers:         peers,
		variableMap:   make(map[string]TransactionDependencyInfo),
		batchQueue:    make([]*PrepareRequest, 0, maxBatchSize),
		batchTimeout:  batchTimeout,
		maxBatchSize:  maxBatchSize,
		lastBatchTime: time.Now(),
		proposeC:      make(chan *PrepareRequest, 1000),
		commitC:       make(chan *PrepareProof, 1000),
		errorC:        make(chan error, 10),
		stopC:         make(chan struct{}),
		messagesC:     make(chan []raftpb.Message, 100),
	}

	go sl.runRaft()
	go sl.runBatcher()

	return sl, nil
}

// runRaft handles Raft consensus events
func (sl *ShardLeader) runRaft() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sl.node.Tick()

		case rd := <-sl.node.Ready():
			if !raft.IsEmptySnap(rd.Snapshot) {
				sl.storage.ApplySnapshot(rd.Snapshot)
			}
			sl.storage.Append(rd.Entries)

			if len(rd.Messages) > 0 {
				select {
				case sl.messagesC <- rd.Messages:
				case <-sl.stopC:
					return
				}
			}

			for _, entry := range rd.CommittedEntries {
				if entry.Type == raftpb.EntryNormal && len(entry.Data) > 0 {
					sl.applyEntry(entry)
				}
			}

			sl.node.Advance()

		case req := <-sl.proposeC:
			sl.batchLock.Lock()
			sl.batchQueue = append(sl.batchQueue, req)
			shouldFlush := len(sl.batchQueue) >= sl.maxBatchSize
			sl.batchLock.Unlock()

			if shouldFlush {
				sl.flushBatch()
			}

		case <-sl.stopC:
			sl.node.Stop()
			return
		}
	}
}

// runBatcher batches prepare requests
func (sl *ShardLeader) runBatcher() {
	ticker := time.NewTicker(sl.batchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sl.flushBatch()
		case <-sl.stopC:
			return
		}
	}
}

// flushBatch proposes batched requests to Raft
func (sl *ShardLeader) flushBatch() {
	sl.batchLock.Lock()
	if len(sl.batchQueue) == 0 {
		sl.batchLock.Unlock()
		return
	}

	batch := sl.batchQueue
	sl.batchQueue = make([]*PrepareRequest, 0, sl.maxBatchSize)
	sl.lastBatchTime = time.Now()
	sl.batchLock.Unlock()

	data, err := sl.serializeBatch(batch)
	if err != nil {
		logger.Errorf("Failed to serialize batch for shard %s: %v", sl.shardID, err)
		return
	}

	if err := sl.node.Propose(context.TODO(), data); err != nil {
		logger.Errorf("Failed to propose batch for shard %s: %v", sl.shardID, err)
	}
}

// serializeBatch serializes a batch of prepare requests
func (sl *ShardLeader) serializeBatch(batch []*PrepareRequest) ([]byte, error) {
	pbBatch := &PrepareRequestBatch{
		Requests: make([]*PrepareRequestProto, len(batch)),
	}

	for i, req := range batch {
		readSet := make(map[string][]byte)
		writeSet := make(map[string][]byte)

		for k, v := range req.ReadSet {
			readSet[k] = v
		}
		for k, v := range req.WriteSet {
			writeSet[k] = v
		}

		pbBatch.Requests[i] = &PrepareRequestProto{
			TxID:      req.TxID,
			ShardID:   req.ShardID,
			ReadSet:   readSet,
			WriteSet:  writeSet,
			Timestamp: req.Timestamp.Unix(),
		}
	}

	return pbBatch.Marshal()
}

// applyEntry applies a committed Raft entry
func (sl *ShardLeader) applyEntry(entry raftpb.Entry) {
	sl.commitIndex = entry.Index

	batch := &PrepareRequestBatch{}
	if err := batch.Unmarshal(entry.Data); err != nil {
		logger.Errorf("Failed to unmarshal batch for shard %s: %v", sl.shardID, err)
		return
	}

	for _, reqProto := range batch.Requests {
		hasDependency, dependentTxID := sl.checkDependencies(reqProto)

		proof := &PrepareProof{
			TxID:        reqProto.TxID,
			ShardID:     sl.shardID,
			CommitIndex: sl.commitIndex,
			LeaderID:    sl.node.Status().Lead,
			Term:        entry.Term,
			Signature:   sl.signProof(reqProto.TxID, sl.commitIndex),
		}

		sl.updateDependencyMap(reqProto, hasDependency, dependentTxID, entry.Index)

		select {
		case sl.commitC <- proof:
			logger.Debugf("Shard %s: Sent proof for tx %s at index %d", sl.shardID, reqProto.TxID, entry.Index)
		default:
			logger.Warnf("Commit channel full for shard %s", sl.shardID)
		}

		sl.mu.Lock()
		sl.requestsHandled++
		sl.mu.Unlock()
	}
}

// checkDependencies checks if transaction has dependencies
func (sl *ShardLeader) checkDependencies(req *PrepareRequestProto) (bool, string) {
	sl.variableMapLock.RLock()
	defer sl.variableMapLock.RUnlock()

	hasDependency := false
	dependentTxID := ""

	for key := range req.ReadSet {
		if depInfo, exists := sl.variableMap[key]; exists {
			hasDependency = true
			dependentTxID = depInfo.DependentTxID
			logger.Debugf("Shard %s: Tx %s has read dependency on %s for key %s",
				sl.shardID, req.TxID, dependentTxID, key)
			break
		}
	}

	if !hasDependency {
		for key := range req.WriteSet {
			if depInfo, exists := sl.variableMap[key]; exists {
				hasDependency = true
				dependentTxID = depInfo.DependentTxID
				logger.Debugf("Shard %s: Tx %s has write dependency on %s for key %s",
					sl.shardID, req.TxID, dependentTxID, key)
				break
			}
		}
	}

	return hasDependency, dependentTxID
}

// updateDependencyMap updates the shard's dependency tracking
func (sl *ShardLeader) updateDependencyMap(req *PrepareRequestProto, hasDep bool, depTxID string, commitIndex uint64) {
	sl.variableMapLock.Lock()
	defer sl.variableMapLock.Unlock()

	expiryTime := time.Now().Add(DefaultExpiryDuration)

	for key := range req.WriteSet {
		sl.variableMap[key] = TransactionDependencyInfo{
			Value:         req.WriteSet[key],
			DependentTxID: req.TxID,
			ExpiryTime:    expiryTime,
			HasDependency: hasDep,
		}
		logger.Debugf("Shard %s: Updated dependency map for key %s -> tx %s at index %d",
			sl.shardID, key, req.TxID, commitIndex)
	}
}

// signProof creates a signature for the proof
func (sl *ShardLeader) signProof(txID string, commitIndex uint64) []byte {
	data := fmt.Sprintf("%s:%d:%s", sl.shardID, commitIndex, txID)
	return []byte(data)
}

// HandleAbort handles abort requests
func (sl *ShardLeader) HandleAbort(txID string) error {
	abortData := &AbortEntry{
		TxID:      txID,
		Timestamp: time.Now().Unix(),
	}

	data, err := abortData.Marshal()
	if err != nil {
		return err
	}

	return sl.node.Propose(context.TODO(), data)
}

// ProposeC returns the propose channel
func (sl *ShardLeader) ProposeC() chan<- *PrepareRequest {
	return sl.proposeC
}

// CommitC returns the commit channel
func (sl *ShardLeader) CommitC() <-chan *PrepareProof {
	return sl.commitC
}

// GetRequestsHandled returns the number of requests handled
func (sl *ShardLeader) GetRequestsHandled() int64 {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.requestsHandled
}

// MessagesC returns the channel for outgoing Raft messages
func (sl *ShardLeader) MessagesC() <-chan []raftpb.Message {
	return sl.messagesC
}

// Step advances the state machine using the given message
func (sl *ShardLeader) Step(ctx context.Context, msg raftpb.Message) error {
	return sl.node.Step(ctx, msg)
}

// Stop gracefully stops the shard leader
func (sl *ShardLeader) Stop() {
	close(sl.stopC)
	sl.node.Stop()
}
