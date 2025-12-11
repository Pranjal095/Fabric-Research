
HYPERLEDGER FABRIC - TRANSACTION DEPENDENCY TRACKING IMPLEMENTATION
===============================================================================

AUTHOR: jimmy2683
DATE: 2024
PROJECT: Sharded Raft-based Transaction Dependency Tracking for Hyperledger Fabric


TABLE OF CONTENTS
================================================================================
1. Overview
2. Architecture Design
3. Implementation Steps
4. File Changes Summary
5. New Files Created
6. Testing Guide
7. Configuration Guide
8. Troubleshooting

# 1. OVERVIEW


This implementation adds a sophisticated transaction dependency tracking system
to Hyperledger Fabric's endorser component. The system evolved from a simple
single-leader approach to a more scalable sharded, Raft-based architecture.

KEY OBJECTIVES:
- Track transaction dependencies based on read/write sets
- Prevent transaction conflicts through dependency detection
- Scale horizontally using contract-based sharding
- Ensure fault tolerance through Raft consensus
- Maintain backward compatibility with existing Fabric deployments

EVOLUTION:
Phase 1: Simple hashmap-based dependency tracking in endorser
Phase 2: Leader-follower architecture with circuit breaker
Phase 3: Contract-based sharding with Raft consensus (Current)


# 2. ARCHITECTURE DESIGN


2.1 HIGH-LEVEL ARCHITECTURE
----------------------------
```
┌─────────────────────────────────────────────────────────────┐
│                    CLIENT APPLICATIONS                       │
└────────────────────┬────────────────────────────────────────┘
                     │ Transaction Proposals
                     ▼
┌─────────────────────────────────────────────────────────────┐
│                    ENDORSER LAYER                            │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ Endorser 1   │  │ Endorser 2   │  │ Endorser 3   │      │
│  │ (Leader)     │  │ (Follower)   │  │ (Follower)   │      │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘      │
│         │                  │                  │              │
│         │    Process & Track Dependencies    │              │
│         └──────────────┬───────────────────┬─┘              │
└────────────────────────┼───────────────────┼────────────────┘
                         │                   │
                         ▼                   ▼
┌─────────────────────────────────────────────────────────────┐
│                  SHARD MANAGER                               │
│                                                              │
│  ┌────────────────────────────────────────────────────┐    │
│  │  Contract-Based Sharding                            │    │
│  │                                                      │    │
│  │  ┌─────────────────┐      ┌─────────────────┐     │    │
│  │  │ Shard 1         │      │ Shard 2         │     │    │
│  │  │ (Contract A)    │      │ (Contract B)    │     │    │
│  │  │                 │      │                 │     │    │
│  │  │ Raft Group:     │      │ Raft Group:     │     │    │
│  │  │  ├─ Leader      │      │  ├─ Leader      │     │    │
│  │  │  ├─ Follower 1  │      │  ├─ Follower 1  │     │    │
│  │  │  └─ Follower 2  │      │  └─ Follower 2  │     │    │
│  │  │                 │      │                 │     │    │
│  │  │ Dependencies:   │      │ Dependencies:   │     │    │
│  │  │  key1 -> tx1    │      │  key5 -> tx5    │     │    │
│  │  │  key2 -> tx2    │      │  key6 -> tx6    │     │    │
│  │  └─────────────────┘      └─────────────────┘     │    │
│  └────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
```
2.2 COMPONENT DESCRIPTIONS
----------------------------

ENDORSER:
- Receives transaction proposals from clients
- Simulates transactions and extracts read/write sets
- Coordinates with ShardManager for dependency resolution
- Returns endorsed proposals with dependency information

SHARD MANAGER:
- Manages lifecycle of all contract-based shards
- Provides shard discovery and creation
- Aggregates metrics across shards
- Handles graceful shutdown

SHARD LEADER (Raft-based):
- Runs Raft consensus within each shard
- Tracks dependencies for specific contract
- Batches prepare requests for efficiency
- Provides prepare proofs to endorsers
- Handles abort scenarios

CIRCUIT BREAKER:
- Protects against cascading failures
- Three states: Closed, Open, HalfOpen
- Automatic recovery attempts
- Configurable thresholds

HEALTH CHECKER:
- Periodic health status monitoring
- Leader connectivity verification
- Dependency map size tracking
- Channel status monitoring

2.3 TRANSACTION FLOW WITH DEPENDENCIES
---------------------------------------

STEP 1: Client Sends Proposal
   Client -> Endorser: Transaction Proposal

STEP 2: Endorser Simulates Transaction
   Endorser simulates chaincode execution
   Extracts read/write sets from simulation results

STEP 3: Extract Dependencies
   Endorser identifies:
   - Read keys (variables being read)
   - Write keys (variables being modified)
   - Formats: namespace:key or namespace:collection:key

STEP 4: Determine Contract/Shard
   Contract name = Chaincode name
   ShardManager retrieves or creates shard for contract

STEP 5: Prepare Request to Shard
   Endorser creates PrepareRequest:
   - TxID
   - ShardID (contract name)
   - ReadSet (map of keys to values)
   - WriteSet (map of keys to values)
   - Timestamp

STEP 6: Shard Processes via Raft
   a. Request batched (max 20 requests or 300ms timeout)
   b. Leader proposes batch to Raft group
   c. Raft consensus achieved
   d. Batch committed to Raft log
   e. Leader applies committed entries

STEP 7: Dependency Detection
   For each request in batch:
   - Check ReadSet keys against existing writes
   - Check WriteSet keys against existing writes
   - Identify conflicts and dependencies

STEP 8: Create Prepare Proof
   Shard creates proof:
   - TxID
   - ShardID
   - CommitIndex (Raft log position)
   - LeaderID
   - Term (Raft term)
   - Signature (cryptographic proof)

STEP 9: Return Proof to Endorser
   Shard -> Endorser: PrepareProof
   Endorser verifies proof signature

STEP 10: Create Endorsement
   Endorser creates ProposalResponse:
   - Simulation results
   - Endorsement signature
   - Dependency info in response message:
     * HasDependency: boolean
     * DependentTxID: string
     * ShardCommitIndex: uint64
     * ProofTerm: uint64

STEP 11: Return to Client
   Endorser -> Client: Endorsed Proposal with dependency info

STEP 12: Client Submits to Orderer
   Client can use dependency info to:
   - Order transactions correctly
   - Retry dependent transactions
   - Implement custom conflict resolution

2.4 DATA STRUCTURES
-------------------
```cpp
TransactionDependencyInfo:
{
    Value:         []byte    // Current value of the variable
    DependentTxID: string    // Transaction that last modified this variable
    ExpiryTime:    time.Time // When this entry expires
    HasDependency: bool      // Whether transaction has dependencies
}

PrepareRequest:
{
    TxID:      string              // Transaction identifier
    ShardID:   string              // Contract/chaincode name
    ReadSet:   map[string][]byte   // Keys being read
    WriteSet:  map[string][]byte   // Keys being written
    Timestamp: time.Time           // Request timestamp
}

PrepareProof:
{
    TxID:        string  // Transaction identifier
    ShardID:     string  // Contract/chaincode name
    CommitIndex: uint64  // Raft log position
    LeaderID:    uint64  // Raft leader ID
    Signature:   []byte  // Cryptographic signature
    Term:        uint64  // Raft term
}
```

# 3. IMPLEMENTATION STEPS

PHASE 1: INITIAL DEPENDENCY TRACKING (COMPLETED)
-------------------------------------------------
Implemented basic hashmap-based dependency tracking in endorser.go

Files Modified:
- core/endorser/endorser.go

Features Added:
- VariableMap for tracking state variables
- extractTransactionDependencies() function
- Dependency info in endorsement response
- Basic expiry mechanism

PHASE 2: LEADER-FOLLOWER ARCHITECTURE (COMPLETED)
--------------------------------------------------
Added leader-follower pattern with circuit breaker

Files Modified:
- core/endorser/endorser.go

Features Added:
- EndorserRole (Leader/Follower)
- EndorserConfig
- CircuitBreaker pattern
- Health checking
- Leader connectivity verification

PHASE 3: SHARDING PACKAGE CREATION (COMPLETED)
-----------------------------------------------
Created dedicated sharding package for separation of concerns

New Directory:
- core/endorser/sharding/

New Files:
- core/endorser/sharding/shard_leader.go
- core/endorser/sharding/shard_manager.go

Features:
- Contract-based sharding
- Raft consensus per shard
- Batch processing
- Dynamic shard creation

PHASE 4: REFACTORING ENDORSER (COMPLETED)
------------------------------------------
Split monolithic endorser.go into modular components

Original File: endorser.go (2782 lines)

Split Into:
- core/endorser/endorser.go (~500 lines) - Core logic
- core/endorser/circuit_breaker.go (~100 lines)
- core/endorser/health_check.go (~100 lines)
- core/endorser/transaction_processor.go (~120 lines)
- core/endorser/chaincode.go (~150 lines)

PHASE 5: INTEGRATION (COMPLETED)
---------------------------------
Integrated sharding with endorser component

Files Modified:
- core/endorser/endorser.go
- core/endorser/metrics.go

Changes:
- Added ShardManager to Endorser struct
- Modified ProcessProposalSuccessfullyOrError()
- Added verifyProof() function
- Integrated prepare request/proof flow

PHASE 6: TESTING FRAMEWORK (IN PROGRESS)
-----------------------------------------
Create comprehensive test suite

Planned Files:
- core/endorser/sharding/shard_leader_test.go
- core/endorser/sharding/shard_manager_test.go
- core/endorser/circuit_breaker_test.go
- core/endorser/endorser_integration_test.go


# 4. FILE CHANGES SUMMARY


4.1 ORIGINAL ENDORSER.GO (BEFORE CHANGES)
------------------------------------------
File: core/endorser/endorser.go
Lines: ~1200 (original Fabric code)

Key Components:
- Endorser struct with basic fields
- ProcessProposal() function
- ProcessProposalSuccessfullyOrError() function
- simulateProposal() function
- callChaincode() function
- Support for private data
- ACL checking
- Proposal validation

What Was Added to Original:
- TransactionDependencyInfo struct
- DependencyInfo struct
- VariableMap (map[string]TransactionDependencyInfo)
- extractTransactionDependencies() function
- Dependency tracking in ProcessProposalSuccessfullyOrError()
- Dependency info in response message
- EndorserRole enum
- EndorserConfig struct
- CircuitBreaker implementation
- HealthStatus struct
- Health checking functions
- Leader-follower logic

Original Size: ~1200 lines
After Phase 1-2: ~2782 lines (with duplicates and mess)

4.2 CURRENT ENDORSER.GO (AFTER REFACTORING)
--------------------------------------------
File: core/endorser/endorser.go
Lines: ~500 lines (cleaned and modular)
```
Current Structure:
┌─────────────────────────────────────────┐
│ IMPORTS & PACKAGE DECLARATION           │
├─────────────────────────────────────────┤
│ CONSTANTS                               │
│ - DefaultPrepareTimeout                 │
├─────────────────────────────────────────┤
│ TYPE DEFINITIONS                        │
│ - TransactionDependencyInfo             │
│ - DependencyInfo                        │
│ - PrivateDataDistributor (interface)    │
│ - Support (interface)                   │
│ - ChannelFetcher (interface)            │
│ - Channel                               │
│ - EndorserRole                          │
│ - EndorserConfig                        │
│ - Endorser (main struct)                │
├─────────────────────────────────────────┤
│ CONSTRUCTOR                             │
│ - NewEndorser()                         │
├─────────────────────────────────────────┤
│ LIFECYCLE METHODS                       │
│ - Shutdown()                            │
├─────────────────────────────────────────┤
│ CORE ENDORSEMENT LOGIC                  │
│ - ProcessProposal()                     │
│ - ProcessProposalSuccessfullyOrError()  │
│ - verifyProof()                         │
├─────────────────────────────────────────┤
│ VALIDATION & PREPROCESSING              │
│ - preProcess()                          │
├─────────────────────────────────────────┤
│ DEPENDENCY EXTRACTION                   │
│ - extractTransactionDependencies()      │
├─────────────────────────────────────────┤
│ UTILITY FUNCTIONS                       │
│ - buildChaincodeInterest()              │
│ - acquireTxSimulator()                  │
│ - shorttxid()                           │
│ - CreateCCEventBytes()                  │
│ - decorateLogger()                      │
└─────────────────────────────────────────┘
```
Key Differences from Original:
1. Added ShardManager integration
2. Modified endorsement flow to use sharded Raft
3. Removed duplicate code
4. Moved circuit breaker to separate file
5. Moved health check to separate file
6. Moved transaction processing to separate file
7. Moved chaincode execution to separate file
8. Cleaner imports
9. Better code organization
10. Comprehensive comments

Removed/Moved:
- CircuitBreaker struct -> circuit_breaker.go
- HealthStatus methods -> health_check.go
- processTransactions() -> transaction_processor.go
- cleanupExpiredDependencies() -> transaction_processor.go
- callChaincode() -> chaincode.go
- simulateProposal() -> chaincode.go

4.3 MODIFIED FILES DETAILS
---------------------------

FILE: core/endorser/endorser.go
Before: 2782 lines (with duplicates)
After: ~500 lines
Changes:
  - Removed duplicate CircuitBreaker definition
  - Removed duplicate function implementations
  - Added ShardManager field to Endorser struct
  - Modified ProcessProposalSuccessfullyOrError() to use sharding
  - Added verifyProof() for shard proof validation
  - Cleaned up imports (removed grpc, insecure, added sharding)
  - Improved documentation
  - Removed leader-follower specific code (moved to legacy support)

FILE: core/endorser/metrics.go
Changes:
  - Added ExpiredDependenciesRemoved (metrics.Counter)
  - Added DependencyMapSize (metrics.Gauge)
  - Added TransactionsWithDependencies (metrics.Counter)
  - Added LeaderCircuitBreakerOpen (metrics.Counter)
  - Added LeaderCircuitBreakerClosed (metrics.Counter)
  - Added LeaderCircuitBreakerHalfOpen (metrics.Counter)
  - Added ShardRequestsHandled (metrics.Counter per shard)


# 5. NEW FILES CREATED

5.1 SHARDING PACKAGE FILES
---------------------------

FILE: core/endorser/sharding/shard_leader.go
Lines: ~350
Purpose: Raft-based shard leader implementation

Contents:
  IMPORTS:
  - context
  - fmt
  - sync
  - time
  - proto (golang/protobuf)
  - raft (etcd/raft/v3)
  - raftpb (etcd/raft/v3/raftpb)
  - pb (fabric-protos-go/peer)
  - flogging (fabric/common)

  CONSTANTS:
  - DefaultBatchTimeout = 300ms
  - DefaultBatchMaxSize = 20
  - DefaultExpiryDuration = 5 minutes

  TYPE DEFINITIONS:
  - ShardConfig struct
    * ShardID string
    * ReplicaNodes []string
    * ReplicaID uint64

  - TransactionDependencyInfo struct
    * Value []byte
    * DependentTxID string
    * ExpiryTime time.Time
    * HasDependency bool

  - PrepareRequest struct
    * TxID string
    * ShardID string
    * ReadSet map[string][]byte
    * WriteSet map[string][]byte
    * Timestamp time.Time

  - PrepareProof struct
    * TxID string
    * ShardID string
    * CommitIndex uint64
    * LeaderID uint64
    * Signature []byte
    * Term uint64

  - ShardLeader struct
    * shardID string
    * node raft.Node
    * storage *raft.MemoryStorage
    * peers []raft.Peer
    * variableMap map[string]TransactionDependencyInfo
    * variableMapLock sync.RWMutex
    * batchQueue []*PrepareRequest
    * batchLock sync.Mutex
    * batchTimeout time.Duration
    * maxBatchSize int
    * lastBatchTime time.Time
    * proposeC chan *PrepareRequest
    * commitC chan *PrepareProof
    * errorC chan error
    * stopC chan struct{}
    * commitIndex uint64
    * mu sync.RWMutex
    * requestsHandled int64

  FUNCTIONS:
  - NewShardLeader(config, batchTimeout, maxBatchSize) (*ShardLeader, error)
    Creates new Raft-based shard leader
    Initializes Raft node with peers
    Starts runRaft() and runBatcher() goroutines

  - runRaft()
    Main Raft event loop
    Handles Ready() channel
    Processes committed entries
    Manages Raft state

  - runBatcher()
    Batches prepare requests for efficiency
    Flushes on timeout or max size

  - flushBatch()
    Serializes batch
    Proposes to Raft

  - serializeBatch(batch) ([]byte, error)
    Converts batch to protobuf

  - applyEntry(entry)
    Applies committed Raft entry
    Checks dependencies
    Creates proof
    Updates dependency map
    Sends proof to endorser

  - checkDependencies(req) (bool, string)
    Checks read-write conflicts
    Checks write-write conflicts
    Returns dependency info

  - updateDependencyMap(req, hasDep, depTxID, commitIndex)
    Updates shard's dependency tracking
    Sets expiry time

  - signProof(txID, commitIndex) []byte
    Creates cryptographic signature for proof
    NOTE: Simplified implementation, use real crypto in production

  - HandleAbort(txID) error
    Handles transaction abort
    Proposes abort to Raft

  - Stop()
    Graceful shutdown
    Closes channels
    Stops Raft node

  - ProposeC() chan *PrepareRequest
    Returns propose channel

  - CommitC() chan *PrepareProof
    Returns commit channel

FILE: core/endorser/sharding/shard_manager.go
Lines: ~120
Purpose: Centralized shard lifecycle management

Contents:
  TYPE DEFINITIONS:
  - ShardManager struct
    * shards map[string]*ShardLeader
    * shardsLock sync.RWMutex
    * config map[string]ShardConfig
    * metrics *Metrics

  FUNCTIONS:
  - NewShardManager(configs, metrics) *ShardManager
    Creates shard manager
    Initializes configured shards
    Logs initialization status

  - GetOrCreateShard(contractName) (*ShardLeader, error)
    Retrieves existing shard or creates new one
    Thread-safe with double-check locking
    Creates default config for dynamic shards

  - Shutdown()
    Stops all shards gracefully
    Logs shutdown progress

  - GetShardMetrics() map[string]int64
    Aggregates metrics from all shards
    Returns requests handled per shard

5.2 ENDORSER MODULE FILES
--------------------------

FILE: core/endorser/circuit_breaker.go
Lines: ~100
Purpose: Circuit breaker pattern implementation

Contents:
  CONSTANTS:
  - Default configuration values

  TYPE DEFINITIONS:
  - CircuitState int
    * CircuitClosed (0)
    * CircuitOpen (1)
    * CircuitHalfOpen (2)

  - CircuitBreakerConfig struct
    * Threshold int
    * Timeout time.Duration
    * MaxRetries int
    * RetryInterval time.Duration

  - CircuitBreaker struct
    * failures int
    * lastFailureTime time.Time
    * config CircuitBreakerConfig
    * state CircuitState
    * mu sync.RWMutex
    * metrics *Metrics
    * retryCount int

  FUNCTIONS:
  - DefaultCircuitBreakerConfig() CircuitBreakerConfig
    Returns default configuration
    Threshold: 5 failures
    Timeout: 30 seconds
    MaxRetries: 3
    RetryInterval: 5 seconds

  - NewCircuitBreaker(config, metrics) *CircuitBreaker
    Creates new instance
    Initializes in Closed state

  - Execute(operation func() error) error
    Wraps operation with circuit breaker logic
    Manages state transitions
    Updates metrics

  - GetState() CircuitState
    Thread-safe state retrieval

FILE: core/endorser/health_check.go
Lines: ~100
Purpose: Health monitoring and diagnostics

Contents:
  IMPORTS:
  - fmt, time
  - grpc, insecure credentials

  TYPE DEFINITIONS:
  - HealthStatus struct
    * IsHealthy bool
    * LastCheckTime time.Time
    * Details map[string]interface{}

  FUNCTIONS:
  - runHealthChecks()
    Periodic health check loop (30 second interval)
    Runs until stopChan closed

  - performHealthCheck()
    Performs comprehensive health check
    Checks dependency map size
    Checks leader connectivity (if follower)
    Checks channel status
    Updates HealthStatus

  - checkLeaderConnectivity() error
    Verifies leader is reachable
    Uses circuit breaker
    Caches result for 30 seconds

  - GetHealthStatus() *HealthStatus
    Thread-safe status retrieval

FILE: core/endorser/transaction_processor.go
Lines: ~120
Purpose: Transaction processing logic for leader

Contents:
  IMPORTS:
  - fmt, time, proto
  - kvrwset, pb
  - util

  FUNCTIONS:
  - cleanupExpiredDependencies()
    Periodic cleanup (1 minute interval)
    Identifies expired entries
    Removes in two-phase (read then write)
    Updates metrics

  - processTransactions()
    Main transaction processing loop
    Receives from TxChannel
    Processes and returns to ResponseChannel

  - extractDependencyInfo(tx) (*DependencyInfo, error)
    Extracts chaincode action
    Unmarshals read/write set
    Checks for dependencies in VariableMap

  - processTransaction(tx) (*pb.ProposalResponse, error)
    Generates transaction ID
    Extracts dependency info
    Updates VariableMap
    Adds dependency info to response

FILE: core/endorser/chaincode.go
Lines: ~150
Purpose: Chaincode execution logic

Contents:
  IMPORTS:
  - fmt, time
  - shim, pb
  - ccprovider, ledger
  - protoutil, errors
  - zap

  FUNCTIONS:
  - callChaincode(txParams, input, chaincodeName) (*pb.Response, *pb.ChaincodeEvent, error)
    Executes chaincode (system or user)
    Handles LSCC special cases
    Manages metrics
    Logs execution time

  - simulateProposal(txParams, chaincodeName, chaincodeInput) (...)
    Simulates chaincode execution
    Manages transaction simulator lifecycle
    Handles private data distribution
    Builds chaincode interest
    Returns simulation results

FILE: core/endorser/sharding/constants.go (NEW - RECOMMENDED)
Lines: ~20
Purpose: Centralized constants

Suggested Contents:
  const (
      DefaultBatchTimeout     = 300 * time.Millisecond
      DefaultBatchMaxSize     = 20
      DefaultPrepareTimeout   = 2000 * time.Millisecond
      DefaultExpiryDuration   = 5 * time.Minute
      DefaultRaftElectionTick = 10
      DefaultRaftHeartbeatTick = 1
      MaxChannelBuffer        = 1000
  )

# 6. TESTING GUIDE

6.1 PREREQUISITES
-----------------
```cpp
Install Dependencies:
  cd /home/jimmy2683/HyperLedger
  go mod download
  go mod tidy

Install Testing Tools:
  go install github.com/onsi/ginkgo/v2/ginkgo@latest
  go install github.com/onsi/gomega@latest

Verify Installation:
  ginkgo version
  go test -v ./... -run=TestNothing (should show no tests)
```
6.2 UNIT TESTS
--------------
```cpp
TEST 1: Shard Leader Functionality
Location: core/endorser/sharding/shard_leader_test.go
Command: cd core/endorser/sharding && go test -v -run TestShardLeader

Create File: shard_leader_test.go
package sharding_test

import (
    "testing"
    "time"
    
    "github.com/hyperledger/fabric/core/endorser/sharding"
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
)

func TestSharding(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "Sharding Suite")
}

var _ = Describe("ShardLeader", func() {
    var (
        shard *sharding.ShardLeader
        config sharding.ShardConfig
    )
    
    BeforeEach(func() {
        config = sharding.ShardConfig{
            ShardID: "testContract",
            ReplicaNodes: []string{"node1", "node2", "node3"},
            ReplicaID: 1,
        }
        var err error
        shard, err = sharding.NewShardLeader(config, 300*time.Millisecond, 20)
        Expect(err).ToNot(HaveOccurred())
    })
    
    AfterEach(func() {
        shard.Stop()
    })
    
    It("should create a shard leader", func() {
        Expect(shard).ToNot(BeNil())
    })
    
    It("should handle prepare requests", func() {
        req := &sharding.PrepareRequest{
            TxID: "tx1",
            ShardID: "testContract",
            WriteSet: map[string][]byte{"key1": []byte("value1")},
            Timestamp: time.Now(),
        }
        
        shard.ProposeC() <- req
        
        select {
        case proof := <-shard.CommitC():
            Expect(proof).ToNot(BeNil())
            Expect(proof.TxID).To(Equal("tx1"))
        case <-time.After(5 * time.Second):
            Fail("Timeout waiting for proof")
        }
    })
    
    It("should detect dependencies", func() {
        // First transaction
        req1 := &sharding.PrepareRequest{
            TxID: "tx1",
            ShardID: "testContract",
            WriteSet: map[string][]byte{"key1": []byte("value1")},
            Timestamp: time.Now(),
        }
        shard.ProposeC() <- req1
        <-shard.CommitC()
        
        // Second transaction with dependency
        req2 := &sharding.PrepareRequest{
            TxID: "tx2",
            ShardID: "testContract",
            ReadSet: map[string][]byte{"key1": []byte("value1")},
            Timestamp: time.Now(),
        }
        shard.ProposeC() <- req2
        
        proof := <-shard.CommitC()
        Expect(proof.CommitIndex).To(BeNumerically(">", 1))
    })
})
```
```cpp
TEST 2: Shard Manager
Location: core/endorser/sharding/shard_manager_test.go
Command: cd core/endorser/sharding && go test -v -run TestShardManager

Create File: shard_manager_test.go
package sharding_test

import (
    "github.com/hyperledger/fabric/core/endorser/sharding"
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
)

var _ = Describe("ShardManager", func() {
    var manager *sharding.ShardManager
    
    BeforeEach(func() {
        configs := map[string]sharding.ShardConfig{
            "contract1": {
                ShardID: "contract1",
                ReplicaNodes: []string{"node1", "node2"},
                ReplicaID: 1,
            },
        }
        manager = sharding.NewShardManager(configs, nil)
    })
    
    AfterEach(func() {
        manager.Shutdown()
    })
    
    It("should create shards from config", func() {
        metrics := manager.GetShardMetrics()
        Expect(metrics).To(HaveKey("contract1"))
    })
    
    It("should create shards dynamically", func() {
        shard, err := manager.GetOrCreateShard("newContract")
        Expect(err).ToNot(HaveOccurred())
        Expect(shard).ToNot(BeNil())
    })
    
    It("should return same shard for same contract", func() {
        shard1, _ := manager.GetOrCreateShard("contract1")
        shard2, _ := manager.GetOrCreateShard("contract1")
        Expect(shard1).To(BeIdenticalTo(shard2))
    })
})
```
```cpp
TEST 3: Circuit Breaker
Location: core/endorser/circuit_breaker_test.go
Command: cd core/endorser && go test -v -run TestCircuitBreaker

Create File: circuit_breaker_test.go
package endorser_test

import (
    "errors"
    "testing"
    "time"
    
    "github.com/hyperledger/fabric/core/endorser"
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
)

func TestEndorser(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "Endorser Suite")
}

var _ = Describe("CircuitBreaker", func() {
    var cb *endorser.CircuitBreaker
    
    BeforeEach(func() {
        config := endorser.DefaultCircuitBreakerConfig()
        config.Threshold = 3
        cb = endorser.NewCircuitBreaker(config, nil)
    })
    
    It("should start in closed state", func() {
        Expect(cb.GetState()).To(Equal(endorser.CircuitClosed))
    })
    
    It("should open after threshold failures", func() {
        for i := 0; i < 3; i++ {
            cb.Execute(func() error {
                return errors.New("failure")
            })
        }
        Expect(cb.GetState()).To(Equal(endorser.CircuitOpen))
    })
    
    It("should close after successful execution", func() {
        err := cb.Execute(func() error {
            return nil
        })
        Expect(err).ToNot(HaveOccurred())
        Expect(cb.GetState()).To(Equal(endorser.CircuitClosed))
    })
    
    It("should reject requests when open", func() {
        // Open the circuit
        for i := 0; i < 3; i++ {
            cb.Execute(func() error { return errors.New("fail") })
        }
        
        err := cb.Execute(func() error { return nil })
        Expect(err).To(HaveOccurred())
        Expect(err.Error()).To(ContainSubstring("circuit breaker is open"))
    })
    
    It("should transition to half-open after timeout", func() {
        config := endorser.DefaultCircuitBreakerConfig()
        config.Threshold = 1
        config.Timeout = 100 * time.Millisecond
        cb = endorser.NewCircuitBreaker(config, nil)
        
        cb.Execute(func() error { return errors.New("fail") })
        Expect(cb.GetState()).To(Equal(endorser.CircuitOpen))
        
        time.Sleep(150 * time.Millisecond)
        cb.Execute(func() error { return nil })
        Expect(cb.GetState()).To(Equal(endorser.CircuitClosed))
    })
})
```
```cpp
TEST 4: Run All Unit Tests
Command: cd /home/jimmy2683/HyperLedger/core/endorser && go test -v ./...

Expected Output:
=== RUN   TestSharding
Running Suite: Sharding Suite
Random Seed: 1234
Will run 3 of 3 specs
...
Ran 3 of 3 Specs in 1.234 seconds
SUCCESS! -- 3 Passed | 0 Failed | 0 Pending | 0 Skipped
--- PASS: TestSharding (1.23s)

=== RUN   TestEndorser
Running Suite: Endorser Suite
...
--- PASS: TestEndorser (0.45s)

PASS
ok      github.com/hyperledger/fabric/core/endorser    2.678s
```
6.3 INTEGRATION TESTS
---------------------
```cpp
TEST 5: Full Endorsement Flow
Location: core/endorser/endorser_integration_test.go
Tags: integration
Command: go test -v -tags=integration -run TestEndorserIntegration

Create File: endorser_integration_test.go
// +build integration

package endorser_test

import (
    "context"
    "testing"
    "time"
    
    pb "github.com/hyperledger/fabric-protos-go/peer"
    "github.com/hyperledger/fabric/core/endorser"
    "github.com/hyperledger/fabric/core/endorser/sharding"
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
)

var _ = Describe("Endorser Integration", func() {
    var (
        e *endorser.Endorser
        ctx context.Context
    )
    
    BeforeEach(func() {
        config := endorser.EndorserConfig{
            Role: endorser.LeaderEndorser,
            EndorserID: "endorser1",
        }
        
        // Setup with test dependencies
        e = setupTestEndorser(config)
        ctx = context.Background()
    })
    
    AfterEach(func() {
        e.Shutdown()
    })
    
    It("should process proposal end-to-end", func() {
        proposal := createTestProposal("testCC", "invoke", []string{"a", "b", "100"})
        response, err := e.ProcessProposal(ctx, proposal)
        
        Expect(err).ToNot(HaveOccurred())
        Expect(response).ToNot(BeNil())
        Expect(response.Response.Status).To(Equal(int32(200)))
    })
    
    It("should track dependencies across transactions", func() {
        // First transaction
        prop1 := createTestProposal("testCC", "invoke", []string{"a", "b", "100"})
        resp1, _ := e.ProcessProposal(ctx, prop1)
        Expect(resp1.Response.Message).ToNot(ContainSubstring("HasDependency=true"))
        
        // Second transaction on same keys
        prop2 := createTestProposal("testCC", "invoke", []string{"a", "b", "50"})
        resp2, _ := e.ProcessProposal(ctx, prop2)
        Expect(resp2.Response.Message).To(ContainSubstring("HasDependency=true"))
    })
    
    It("should handle shard-based dependency resolution", func() {
        shard, err := e.ShardManager.GetOrCreateShard("testCC")
        Expect(err).ToNot(HaveOccurred())
        Expect(shard).ToNot(BeNil())
        
        proposal := createTestProposal("testCC", "invoke", []string{"key1", "value1"})
        response, err := e.ProcessProposal(ctx, proposal)
        
        Expect(err).ToNot(HaveOccurred())
        Expect(response.Response.Message).To(ContainSubstring("ShardCommitIndex"))
    })
})

func setupTestEndorser(config endorser.EndorserConfig) *endorser.Endorser {
    // Create mock dependencies
    channelFetcher := &mockChannelFetcher{}
    localMSP := &mockIdentityDeserializer{}
    pvtDataDist := &mockPrivateDataDistributor{}
    support := &mockSupport{}
    pvtRWSetAssembler := &mockPvtRWSetAssembler{}
    metrics := endorser.NewMetrics(&disabled.Provider{})
    
    return endorser.NewEndorser(
        channelFetcher,
        localMSP,
        pvtDataDist,
        support,
        pvtRWSetAssembler,
        metrics,
        config,
    )
}

func createTestProposal(chaincode, function string, args []string) *pb.SignedProposal {
    // Implementation depends on your test setup
    // Should create a valid SignedProposal for testing
    return &pb.SignedProposal{
        // ... test proposal fields
    }
}
```
6.4 BENCHMARK TESTS
-------------------
```cpp
TEST 6: Performance Benchmarks
Location: core/endorser/endorser_bench_test.go
Command: go test -bench=. -benchmem -run=^$ core/endorser

Create File: endorser_bench_test.go
package endorser_test

import (
    "context"
    "testing"
    
    pb "github.com/hyperledger/fabric-protos-go/peer"
    "github.com/hyperledger/fabric/core/endorser"
)

func BenchmarkEndorserThroughput(b *testing.B) {
    e := setupTestEndorser(endorser.EndorserConfig{
        Role: endorser.LeaderEndorser,
    })
    defer e.Shutdown()
    
    ctx := context.Background()
    proposal := createTestProposal("bench", "invoke", []string{"a", "b", "100"})
    
    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            _, err := e.ProcessProposal(ctx, proposal)
            if err != nil {
                b.Fatal(err)
            }
        }
    })
}

func BenchmarkShardLeaderBatching(b *testing.B) {
    config := sharding.ShardConfig{
        ShardID: "benchCC",
        ReplicaNodes: []string{"n1", "n2", "n3"},
        ReplicaID: 1,
    }
    shard, _ := sharding.NewShardLeader(config, 300*time.Millisecond, 20)
    defer shard.Stop()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        req := &sharding.PrepareRequest{
            TxID: fmt.Sprintf("tx%d", i),
            ShardID: "benchCC",
            WriteSet: map[string][]byte{"key": []byte("value")},
            Timestamp: time.Now(),
        }
        shard.ProposeC() <- req
        <-shard.CommitC()
    }
}

func BenchmarkDependencyExtraction(b *testing.B) {
    e := setupTestEndorser(endorser.EndorserConfig{})
    defer e.Shutdown()
    
    simResult := createTestSimulationResult(100) // 100 read/write operations
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, err := e.extractTransactionDependencies(simResult)
        if err != nil {
            b.Fatal(err)
        }
    }
}

Expected Benchmark Output:
BenchmarkEndorserThroughput-8           5000    250000 ns/op    4096 B/op    50 allocs/op
BenchmarkShardLeaderBatching-8          2000    500000 ns/op    8192 B/op   100 allocs/op
BenchmarkDependencyExtraction-8        10000    100000 ns/op    2048 B/op    30 allocs/op
```
6.5 END-TO-END TESTS
--------------------
```cpp
TEST 7: Fabric Network Test
Setup: Requires running Fabric test network

Step 1: Start Test Network
cd /home/jimmy2683/HyperLedger/fabric-samples/test-network
./network.sh down
./network.sh up createChannel -c mychannel -ca

Step 2: Deploy Modified Endorser
# Replace endorser binary with modified version
cp /home/jimmy2683/HyperLedger/build/bin/peer /path/to/fabric-samples/bin/

Step 3: Deploy Test Chaincode
./network.sh deployCC -ccn basic -ccp ../asset-transfer-basic/chaincode-go \
    -ccl go -ccep "OR('Org1MSP.peer','Org2MSP.peer')"

Step 4: Create Test Script
File: test-network/scripts/test-dependencies.sh

#!/bin/bash

set -e

export PATH=${PWD}/../bin:$PATH
export FABRIC_CFG_PATH=$PWD/../config/
export CORE_PEER_TLS_ENABLED=true
export CORE_PEER_LOCALMSPID="Org1MSP"
export CORE_PEER_TLS_ROOTCERT_FILE=${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt
export CORE_PEER_MSPCONFIGPATH=${PWD}/organizations/peerOrganizations/org1.example.com/users/Admin@org1.example.com/msp
export CORE_PEER_ADDRESS=localhost:7051

echo "=== Testing Dependency Tracking ==="

echo "Transaction 1: Create asset1"
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com \
    --tls --cafile ${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem \
    -C mychannel -n basic --peerAddresses localhost:7051 \
    --tlsRootCertFiles ${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt \
    --peerAddresses localhost:9051 \
    --tlsRootCertFiles ${PWD}/organizations/peerOrganizations/org2.example.com/peers/peer0.org2.example.com/tls/ca.crt \
    -c '{"function":"CreateAsset","Args":["asset1","blue","5","Tom","1000"]}' 2>&1 | tee tx1.log

sleep 2

echo "Transaction 2: Transfer asset1 (should have dependency)"
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com \
    --tls --cafile ${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem \
    -C mychannel -n basic --peerAddresses localhost:7051 \
    --tlsRootCertFiles ${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt \
    --peerAddresses localhost:9051 \
    --tlsRootCertFiles ${PWD}/organizations/peerOrganizations/org2.example.com/peers/peer0.org2.example.com/tls/ca.crt \
    -c '{"function":"TransferAsset","Args":["asset1","Jerry"]}' 2>&1 | tee tx2.log

echo "=== Checking for Dependency Info ==="
if grep -q "DependencyInfo:HasDependency=true" tx2.log; then
    echo "SUCCESS: Dependency detected in transaction 2"
else
    echo "FAILURE: No dependency information found"
    exit 1
fi

echo "=== Test Complete ==="

Step 5: Run Test
chmod +x test-network/scripts/test-dependencies.sh
cd test-network
./scripts/test-dependencies.sh

Expected Output:
=== Testing Dependency Tracking ===
Transaction 1: Create asset1
2024-XX-XX XX:XX:XX.XXX UTC [chaincodeCmd] chaincodeInvokeOrQuery -> INFO 001 Chaincode invoke successful. result: status:200

Transaction 2: Transfer asset1 (should have dependency)
2024-XX-XX XX:XX:XX.XXX UTC [chaincodeCmd] chaincodeInvokeOrQuery -> INFO 001 Chaincode invoke successful. result: status:200 message:"DependencyInfo:HasDependency=true,DependentTxID=xxx,ShardCommitIndex=2,ProofTerm=1"

=== Checking for Dependency Info ===
SUCCESS: Dependency detected in transaction 2
=== Test Complete ===
```
6.6 MONITORING & DEBUGGING
---------------------------
```cpp
View Metrics:
curl http://localhost:9443/metrics | grep -E "dependency|shard|circuit"

Sample Metrics Output:
# HELP endorser_dependency_map_size Current size of dependency map
# TYPE endorser_dependency_map_size gauge
endorser_dependency_map_size 150

# HELP endorser_expired_dependencies_removed Total expired dependencies removed
# TYPE endorser_expired_dependencies_removed counter
endorser_expired_dependencies_removed 23

# HELP endorser_transactions_with_dependencies Total transactions with dependencies
# TYPE endorser_transactions_with_dependencies counter
endorser_transactions_with_dependencies{channel="mychannel",chaincode="basic"} 45

# HELP endorser_circuit_breaker_state Circuit breaker state
# TYPE endorser_circuit_breaker_state gauge
endorser_circuit_breaker_closed 1
endorser_circuit_breaker_open 0

Enable Debug Logging:
export FABRIC_LOGGING_SPEC=endorser=debug:sharding=debug

View Logs:
docker logs peer0.org1.example.com 2>&1 | grep -E "dependency|shard|proof"

Expected Log Output:
2024-XX-XX XX:XX:XX.XXX UTC [endorser] ProcessProposalSuccessfullyOrError -> DEBU 001 Found 3 dependencies for transaction
2024-XX-XX XX:XX:XX.XXX UTC [endorser] ProcessProposalSuccessfullyOrError -> DEBU 002 Submitted prepare request for tx txid123 to shard basic
2024-XX-XX XX:XX:XX.XXX UTC [sharding] applyEntry -> DEBU 003 Shard basic: Tx tx124 has read dependency on tx123 for key asset:asset1
2024-XX-XX XX:XX:XX.XXX UTC [sharding] applyEntry -> DEBU 004 Shard basic: Sent proof for tx tx124 at index 2
```

# 7. CONFIGURATION GUIDE

7.1 ENDORSER CONFIGURATION
---------------------------

File: core.yaml (peer configuration)

Add to peer section:
peer:
  endorser:
    role: leader                    # Options: leader, normal
    leaderEndorser: "localhost:7051" # For normal endorsers
    endorserId: "endorser1"         # Unique identifier
    expiryDuration: "5m"            # Dependency expiry time

    # Circuit breaker settings
    circuitBreaker:
      threshold: 5                  # Failures before opening
      timeout: "30s"                # Recovery attempt delay
      maxRetries: 3                 # Max retries in half-open
      retryInterval: "5s"           # Time between retries

    # Shard settings
    sharding:
      batchTimeout: "300ms"         # Batch flush timeout
      batchMaxSize: 20              # Max requests per batch
      prepareTimeout: "2s"          # Timeout for prepare phase
      expiryDuration: "5m"          # Dependency entry TTL

7.2 SHARD CONFIGURATION
-----------------------

File: shard-config.yaml (new file)

shards:
  - shardId: "basic"
    replicaNodes:
      - "peer0.org1.example.com:7051"
      - "peer0.org2.example.com:9051"
      - "peer0.org3.example.com:11051"
    replicaId: 1                    # This node's ID (1-based)
    
  - shardId: "fabcar"
    replicaNodes:
      - "peer0.org1.example.com:7051"
      - "peer0.org2.example.com:9051"
    replicaId: 1

Environment Variables:
export ENDORSER_ROLE=leader
export ENDORSER_ID=endorser1
export ENDORSER_LEADER_ADDRESS=localhost:7051
export ENDORSER_EXPIRY_DURATION=5m
export SHARD_BATCH_TIMEOUT=300ms
export SHARD_BATCH_MAX_SIZE=20

7.3 RAFT CONFIGURATION
----------------------

Raft Parameters (in ShardLeader):
- ElectionTick: 10       # Ticks before election timeout
- HeartbeatTick: 1       # Ticks between heartbeats
- MaxSizePerMsg: 1MB     # Max message size
- MaxInflightMsgs: 256   # Max in-flight append messages

Tuning Guide:
- High latency network: Increase ElectionTick to 15-20
- High throughput: Increase MaxInflightMsgs to 512
- Large transactions: Increase MaxSizePerMsg to 4MB

7.4 METRICS CONFIGURATION
-------------------------

File: core.yaml

metrics:
  provider: prometheus
  statsd:
    network: udp
    address: localhost:8125
    writeInterval: 10s
    prefix: fabric

Prometheus Endpoint:
http://localhost:9443/metrics

Grafana Dashboard:
Import fabric-endorser-dashboard.json


# 8. TROUBLESHOOTING

8.1 COMMON ISSUES
-----------------

ISSUE 1: "timeout submitting to shard"
Cause: Shard leader is overloaded or not responding
Solution:
- Check shard leader logs
- Verify Raft quorum (need majority of replicas)
- Increase PrepareTimeout in configuration
- Check network connectivity between replicas

Debug Commands:
docker logs peer0.org1.example.com | grep "shard.*timeout"
curl http://localhost:9443/metrics | grep shard_requests

ISSUE 2: "circuit breaker is open"
Cause: Leader endorser is unreachable or failing
Solution:
- Verify leader endorser is running
- Check network connectivity
- Review leader logs for errors
- Wait for circuit breaker timeout
- Consider increasing threshold

Debug Commands:
docker ps | grep peer
docker logs peer0.org1.example.com | grep "circuit breaker"
curl http://localhost:9443/metrics | grep circuit_breaker

ISSUE 3: "invalid proof from shard"
Cause: Proof signature verification failed
Solution:
- Ensure all replicas use same shard configuration
- Check for replay attacks
- Verify shard leader term is consistent
- Review Raft logs for split-brain scenarios

Debug Commands:
docker logs peer0.org1.example.com | grep "proof.*invalid"
docker logs peer0.org1.example.com | grep "Raft.*term"

ISSUE 4: Dependency map growing too large
Cause: Expiry mechanism not working or high transaction volume
Solution:
- Reduce EndorsementExpiryDuration
- Verify cleanupExpiredDependencies() is running
- Check metrics for cleanup operations
- Consider sharding by contract

Debug Commands:
curl http://localhost:9443/metrics | grep dependency_map_size
docker logs peer0.org1.example.com | grep "cleanup completed"

ISSUE 5: High latency in endorsements
Cause: Raft consensus overhead or network latency
Solution:
- Tune Raft parameters (ElectionTick, HeartbeatTick)
- Increase BatchTimeout to batch more requests
- Reduce number of Raft replicas
- Use local shard leader when possible

Debug Commands:
curl http://localhost:9443/metrics | grep proposal_duration
docker logs peer0.org1.example.com | grep "finished chaincode"

8.2 DEBUGGING TIPS
------------------

Enable Verbose Logging:
export FABRIC_LOGGING_SPEC=endorser=debug:sharding=debug:raft=info

Trace Transaction Flow:
1. Grep for transaction ID in logs
   docker logs peer0.org1.example.com | grep "txid123"

2. Look for key events:
   - "Submitted prepare request"
   - "Received proof"
   - "Shard.*Sent proof"
   - "dependency identified"

Inspect Raft State:
- No direct inspection in current implementation
- Add HTTP endpoint to expose Raft status:
  func (sl *ShardLeader) GetStatus() raft.Status {
      return sl.node.Status()
  }

Monitor Resource Usage:
docker stats peer0.org1.example.com

Expected Resource Usage:
- CPU: 5-15% under normal load
- Memory: 200-500 MB
- Network: Depends on transaction volume

Check File Descriptors:
docker exec peer0.org1.example.com lsof | wc -l

8.3 RECOVERY PROCEDURES
-----------------------

Recover from Raft Leader Failure:
1. Wait for election timeout (ElectionTick * tick interval)
2. New leader elected automatically
3. Resume operations
4. No manual intervention required

Recover from Endorser Failure:
1. If leader endorser fails:
   - Promote follower to leader (manual configuration change)
   - Update EndorserConfig.Role = LeaderEndorser
   - Restart endorser
   
2. If normal endorser fails:
   - Simply restart endorser
   - Will reconnect to leader automatically

Recover from Shard Corruption:
1. Stop all shard replicas
2. Clear shard data directories
3. Restart with clean state
4. Shard will rebuild from Raft snapshots
5. Note: May lose some dependency tracking info

Clear Dependency Map (Emergency):
1. Stop endorser
2. Remove persistent storage (if any)
3. Restart endorser
4. Map rebuilds as new transactions arrive


# 9. PERFORMANCE TUNING

9.1 OPTIMIZATION STRATEGIES
----------------------------

For High Throughput:
- Increase BatchMaxSize to 50
- Reduce BatchTimeout to 100ms
- Increase MaxInflightMsgs to 512
- Use larger channel buffers (proposeC, commitC)

For Low Latency:
- Reduce BatchTimeout to 50ms
- Keep BatchMaxSize small (10-20)
- Reduce ElectionTick and HeartbeatTick
- Use local shard leaders

For Large Transactions:
- Increase MaxSizePerMsg to 4MB
- Increase PrepareTimeout to 5s
- Increase gRPC max message size

For Memory Efficiency:
- Reduce EndorsementExpiryDuration to 1m
- Implement LRU cache for VariableMap
- Limit number of shards per endorser
- Use smaller channel buffers

9.2 CAPACITY PLANNING
---------------------

Endorser Capacity:
- Max concurrent proposals: ~1000
- Recommended proposals/sec: 100-500
- Memory per proposal: ~10KB
- Expected memory usage: 200-800MB

Shard Capacity:
- Max concurrent requests: ~1000
- Recommended requests/sec: 200-1000
- Memory per dependency entry: ~200 bytes
- Expected map size: 10K-100K entries

Raft Capacity:
- Max entries/sec: ~5000
- Log size growth: ~1MB/1000 entries
- Snapshot interval: Every 10K entries
- Memory for Raft state: 50-200MB

Network Capacity:
- Raft inter-replica: ~10-50 Mbps
- Endorser-Shard: ~5-20 Mbps
- Client-Endorser: Depends on transaction size


# 10. FUTURE ENHANCEMENTS

Planned Features:
1. Persistent storage for dependency map
2. Cross-shard transaction support
3. Dynamic shard rebalancing
4. Advanced conflict resolution strategies
5. Snapshot and restore for Raft state
6. Multi-level sharding (contract + key ranges)
7. Optimistic dependency tracking
8. Parallel dependency resolution
9. Machine learning for conflict prediction
10. Integration with Fabric Gateway


# 11. REFERENCES

Hyperledger Fabric Documentation:
https://hyperledger-fabric.readthedocs.io/

Raft Consensus Algorithm:
https://raft.github.io/

etcd/raft Go Library:
https://github.com/etcd-io/etcd/tree/main/raft

Circuit Breaker Pattern:
https://martinfowler.com/bliki/CircuitBreaker.html

Sharding Strategies:
https://en.wikipedia.org/wiki/Shard_(database_architecture)


# 12. CONTACT & SUPPORT

Author: jimmy2683
Repository: github.com/jimmy2683/Mini-Project-1
Date: 2024

For questions or issues:
1. Check troubleshooting section
2. Review logs with debug logging enabled
3. Check metrics for anomalies
4. Consult Fabric community forums


# END OF DOCUMENT

