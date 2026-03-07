/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package sharding

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hyperledger/fabric/common/flogging"
	"go.etcd.io/etcd/raft/v3"
	"go.etcd.io/etcd/raft/v3/raftpb"
)

var logger = flogging.MustGetLogger("endorser.sharding")

const (
	DefaultBatchMaxSize   = 500
	DefaultBatchTimeout   = 10 * time.Millisecond
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
	TxID          string
	ShardID       string
	CommitIndex   uint64
	LeaderID      uint64
	Signature     []byte
	Term          uint64
	DependentTxID string
	HasDependency bool
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
	subscribers     map[string][]chan *PrepareProof
	pendingTxIDs    map[string]bool
	errorC          chan error
	stopC           chan struct{}
	messagesC       chan []raftpb.Message
	requestsHandled uint64
	mu              sync.RWMutex
}

// NewShardLeader creates a new Raft-based shard leader
func NewShardLeader(config ShardConfig, batchTimeout time.Duration, maxBatchSize int) (*ShardLeader, error) {
	storage := raft.NewMemoryStorage()

	c := &raft.Config{
		ID:              config.ReplicaID,
		ElectionTick:    100, // 100 * 100ms = 10 seconds
		HeartbeatTick:   5,   // 5 * 100ms = 0.5 seconds
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
		proposeC:      make(chan *PrepareRequest, 10000),
		subscribers:   make(map[string][]chan *PrepareProof),
		pendingTxIDs:  make(map[string]bool),
		errorC:        make(chan error, 10),
		stopC:         make(chan struct{}),
		messagesC:     make(chan []raftpb.Message, 10000),
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
				default:
					logger.Warnf("Shard %s: messagesC full, dropping %d Raft messages (will be retransmitted)", sl.shardID, len(rd.Messages))
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
			if !sl.pendingTxIDs[req.TxID] {
				sl.batchQueue = append(sl.batchQueue, req)
				sl.pendingTxIDs[req.TxID] = true
			}
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

	sl.batchLock.Lock()
	for _, req := range batch {
		delete(sl.pendingTxIDs, req.TxID)
	}
	sl.batchLock.Unlock()
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
			TxID:          reqProto.TxID,
			ShardID:       sl.shardID,
			CommitIndex:   sl.commitIndex,
			LeaderID:      sl.node.Status().Lead,
			Term:          entry.Term,
			Signature:     sl.signProof(reqProto.TxID, sl.commitIndex),
			DependentTxID: dependentTxID,
			HasDependency: hasDependency,
		}

		sl.updateDependencyMap(reqProto, hasDependency, dependentTxID, entry.Index)

		sl.mu.RLock()
		subs, exists := sl.subscribers[reqProto.TxID]
		sl.mu.RUnlock()

		if exists {
			for _, ch := range subs {
				func(c chan *PrepareProof) {
					defer func() {
						if r := recover(); r != nil {
							logger.Warnf("Shard %s: recovered from send on closed channel for tx %s", sl.shardID, reqProto.TxID)
						}
					}()
					select {
					case c <- proof:
						logger.Debugf("Shard %s: Sent proof for tx %s at index %d", sl.shardID, reqProto.TxID, entry.Index)
					default:
						logger.Warnf("Commit channel full for tx %s in shard %s", reqProto.TxID, sl.shardID)
					}
				}(ch)
			}
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
	depMap := make(map[string]bool)

	// Must sort keys because Go map iteration is randomized
	// If a tx touches multiple variables with dependencies, different
	// ShardLeaders might select different dependentTxIDs and break consensus!
	var readKeys []string
	for k := range req.ReadSet {
		readKeys = append(readKeys, k)
	}
	sort.Strings(readKeys)

	for _, key := range readKeys {
		if depInfo, exists := sl.variableMap[key]; exists {
			hasDependency = true
			if depInfo.DependentTxID != "" {
				depMap[depInfo.DependentTxID] = true
			}
			logger.Debugf("Shard %s: Tx %s has read dependency on %s for key %s",
				sl.shardID, req.TxID, depInfo.DependentTxID, key)
		}
	}

	var writeKeys []string
	for k := range req.WriteSet {
		writeKeys = append(writeKeys, k)
	}
	sort.Strings(writeKeys)

	for _, key := range writeKeys {
		if depInfo, exists := sl.variableMap[key]; exists {
			hasDependency = true
			if depInfo.DependentTxID != "" {
				depMap[depInfo.DependentTxID] = true
			}
			logger.Debugf("Shard %s: Tx %s has write dependency on %s for key %s",
				sl.shardID, req.TxID, depInfo.DependentTxID, key)
		}
	}

	var depList []string
	for txID := range depMap {
		depList = append(depList, txID)
	}
	sort.Strings(depList)
	dependentTxID := strings.Join(depList, ",")

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

// Subscribe provides a one-time channel for a specific transaction's proof.
// Supports multiple concurrent subscribers (e.g. multi-peer endorsement).
func (sl *ShardLeader) Subscribe(txID string) <-chan *PrepareProof {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	ch := make(chan *PrepareProof, 1)
	sl.subscribers[txID] = append(sl.subscribers[txID], ch)
	return ch
}

// Unsubscribe removes a specific subscription channel.
func (sl *ShardLeader) Unsubscribe(txID string, ch <-chan *PrepareProof) {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	subs, exists := sl.subscribers[txID]
	if !exists {
		return
	}
	for i, sub := range subs {
		if sub == ch {
			sl.subscribers[txID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(sl.subscribers[txID]) == 0 {
		delete(sl.subscribers, txID)
	}
}

// GetRequestsHandled returns the number of requests handled
func (sl *ShardLeader) GetRequestsHandled() uint64 {
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
