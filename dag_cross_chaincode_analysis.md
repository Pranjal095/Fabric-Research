# DAG Construction and Cross-Chaincode Invocations

Based on the architecture implemented in `core/endorser/endorser.go` and `core/committer/committer_impl.go`, here is a concrete breakdown of how the DAG is constructed end-to-end, followed by an analysis of how this architecture behaves (and fundamentally breaks) when dealing with multiple smart-contract invocations (cross-chaincode calls).

---

## 1. End-to-End DAG Construction Flow

The pipeline operates in four distinct phases:

### Phase A: Endorsement Simulation & RW Set Extraction
When a transaction is proposed, the Endorser fully simulates the transaction against the current state DB. The `extractTransactionDependencies()` function parses the resulting `TxSimulationResults`. It flattens all Read and Write operations across all namespaces (chaincodes) into a single, comprehensive `map[string][]byte` representing the transaction's entire footprint.

### Phase B: Sharded Dependency Flagging
The transaction must now be assigned a dependency flag. 
1. The Endorser identifies the **Primary Chaincode** invoked by the client (`contractName := up.ChaincodeName`).
2. It routes the extracted RW Set to the specific **Shard Manager** (Raft Cluster) responsible for that primary chaincode.
3. The Shard Leader parses the RW Set. If it detects a conflict with a pending transaction *it already knows about*, it returns `HasDependency=true` and the parent `DependentTxID`. Otherwise, it returns `HasDependency=false`.
4. This metadata is embedded into the proposal response and sent back to the client.

### Phase C: Ordering
The client submits the endorsed transactions to the Orderer. The Orderer blindly batches them into a Block without altering the dependency metadata.

### Phase D: Committer DAG Execution
When the Committer receives the block, `BuildDAGFromBlock()` parses the embedded dependency flags.
1. **Level 0 (Independent):** Any transaction with `HasDependency=false` is placed in Level 0.
2. **Level N (Dependent):** Any transaction with `HasDependency=true` is placed in a level strictly greater than its parent (`DependentTxID`).
3. **Execution:** The Committer executes Level 0 transactions heavily in **parallel** (using Goroutines bounded by a concurrency limit). Transactions in Level > 0 are executed sequentially afterward, explicitly checking for conflicts only against their declared parent (`checkRWSetConflicts`).

---

## 2. The Flaw: Concrete Argument on Cross-Chaincode Invocations

While elegant for isolated chaincodes, this architecture has a **critical flaw** when transactions invoke multiple smart contracts (e.g., Chaincode A calls `InvokeChaincode("B")`). 

### The Scenario
Assume we have two chaincodes, `ContractA` and `ContractB`.
- **Transaction 1 (Tx1):** Client invokes `ContractA`. During execution, `ContractA` makes a cross-chaincode call to `ContractB` to write to key `K`.
- **Transaction 2 (Tx2):** Client invokes `ContractB` directly, and writes to the exact same key `K`.

Both transactions are submitted concurrently. According to Fabric's standard MVCC control, one should succeed and the other should be rejected due to a read-write conflict on key `K`.

### How the Sharded Architecture Fails

**1. Blind Routing at Endorsement:**
In `endorser.go`, the routing logic explicitly targets the *Target Chaincode Name* of the proposal:
```go
contractName := up.ChaincodeName
shard, err := e.ShardManager.GetOrCreateShard(contractName)
```
- **Tx1** is routed to the **Raft Shard for ContractA**.
- **Tx2** is routed to the **Raft Shard for ContractB**.

**2. Isolated State Ignorance:**
- When Shard A receives Tx1's RW Set (which includes the write to `ContractB:K`), it sees no conflict because Shard A only knows about transactions routed to ContractA. It flags Tx1 as `HasDependency=false`.
- When Shard B receives Tx2's RW Set, it also sees no conflict (because it never saw Tx1). It flags Tx2 as `HasDependency=false`.

**3. Disaster at the DAG Committer:**
At the committer phase, `BuildDAGFromBlock()` sees that both Tx1 and Tx2 have `HasDependency=false`. 
- Both are placed into **Level 0**.
- Because they are in Level 0, the Committer blindly executes them in **parallel**. 
- In `processBlockWithDAG()`, parallel transactions do *not* execute mathematical MVCC checks against each other; the code explicitly only runs `checkRWSetConflicts` for `level > 0`.
- Both transactions are marked as `peer.TxValidationCode_VALID` and pushed directly into the Ledger (`CommitLegacy`).

### Conclusion: Determinism and Consistency are Broken
By relying on the *primary* chaincode name to route dependency checks, the sharded raft architecture creates blind spots. Cross-chaincode calls allow a transaction to mutate a state partition belonging to another shard without that shard's knowledge. 

Because the DAG Committer trusts the Shards perfectly and uses parallel execution for Level 0, these undetected cross-chaincode conflicts bypass all safety checks. This leads to **non-deterministic execution**, race conditions, and **state corruption** (dirty reads, lost updates) across the distributed ledger. 

**Summary:** The architecture strictly assumes that 1 Chaincode = 1 Isolated State Database. The moment `InvokeChaincode` is used to cross that boundary, the sharding logic fails to detect dependencies, resulting in parallel execution of conflicting transactions.

---

## 3. Implementation vs. The Original Plan (Poster)

Based on the `poster.pdf` provided, the original conceptual plan correctly identified and addressed this exact cross-chaincode dilemma. 

In the poster's **"3. Sharded Raft-based design"** section, the algorithm explicitly states:
> *"Endorser simulates the transaction, collects read/write sets, and sends a PrepareRequest **per involved shard**... Endorser either waits for proofs from **all relevant shards** or times out and aborts."*

This proves that the *theoretical design* was fully aware of cross-chaincode invocations. The plan intended for the Endorser to inspect the extracted RW Set, identify every distinct chaincode namespace touched, and multi-cast the dependency checks (`PrepareRequest`) to *every* relevant shard.

However, the **actual implementation** in Go (`core/endorser/endorser.go`) took a significant engineering shortcut. Instead of sending requests to all involved shards, the code statically routes the request based solely on the Primary Target Contract that the client invoked:
```go
contractName := up.ChaincodeName
shard, err := e.ShardManager.GetOrCreateShard(contractName)
// ... sends exactly one prepareReq to this single shard
```

### Why the Implementation Deviated
Implementing a true multi-shard atomic prepare phase is notoriously difficult. If the Endorser must ask both Shard A and Shard B for dependency proofs:
1. **Atomic Rollbacks:** What happens if Shard A grants the dependency but Shard B aborts or times out? Shard A would need a mechanism to rollback its reserved dependency slot to avoid locking up keys unnecessarily. 
2. **Distributed Deadlocks:** If Tx1 asks Shard A then Shard B, and concurrently Tx2 asks Shard B then Shard A, the system can easily deadlock.

The Go codebase bypassed these complex distributed protocol challenges (e.g., Two-Phase Commit across Raft groups) and implemented a simplified MVP. 

**Conclusion:** The `poster.pdf` outlines a meticulously correct architectural design that handles cross-shard dependency tracking perfectly. The Go codebase, however, implements a simplified version that skips the multi-shard fan-out, successfully achieving the high-performance benchmark numbers shown in the poster, but sacrificing cross-chaincode determinism in the process.
