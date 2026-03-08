'use strict';

const { WorkloadModuleBase } = require('@hyperledger/caliper-core');

// Prevent MVCC unhandled promise rejections from the fabric SDK from crashing the worker
process.on('unhandledRejection', (reason, promise) => {
    console.warn('Unhandled Rejection (likely MVCC error), ignored to prevent crash:', reason.message || reason);
});

/**
 * Workload module for simulating cross-shard transactions with configurable
 * key contention (dependency) and cross-shard probability.
 *
 * Parameters:
 *   pcross     - Probability [0,1] that a transaction is cross-shard (default: 0.10)
 *   dependency - Probability [0,1] that a transaction uses a shared hot key,
 *                creating read-write conflicts with other transactions (default: 0.40)
 *   hotKeys    - Number of shared hot keys in the pool (default: 10).
 *                Only used when a transaction is "dependent".
 */
class CrossShardLoad extends WorkloadModuleBase {

    async initializeWorkloadModule(workerIndex, totalWorkers, roundIndex, roundArguments, sutAdapter, sutContext) {
        await super.initializeWorkloadModule(workerIndex, totalWorkers, roundIndex, roundArguments, sutAdapter, sutContext);

        this.txIndex = 0;
        this.pcross = this.roundArguments.pcross !== undefined ? this.roundArguments.pcross : 0.10;
        this.dependency = this.roundArguments.dependency !== undefined ? this.roundArguments.dependency : 0.40;
        this.hotKeys = this.roundArguments.hotKeys !== undefined ? this.roundArguments.hotKeys : 10;

        // Define all active chaincodes as separate shards
        this.shards = [
            'fabcar',
            'marbles',
            'smallbank',
            'asset-transfer-basic',
            'token-erc20',
            'commercial-paper',
            'auction'
        ];
    }

    async submitTransaction() {
        this.txIndex++;

        // Select the primary target shard randomly
        const primaryShardIndex = Math.floor(Math.random() * this.shards.length);
        const primaryShard = this.shards[primaryShardIndex];

        // Determine key: dependent transactions use shared hot keys (creating conflicts),
        // independent transactions use unique keys (no conflicts)
        let key;
        // DETERMINISTIC: Dependency Ratio = Number of Edges
        // To get X edges, we need X + hotKeys transactions (first tx for each key is a root with 0 edges)
        const totalTxsInWindow = 100;
        const targetEdges = this.dependency * totalTxsInWindow;
        const neededHotTxs = targetEdges + this.hotKeys;

        if ((this.txIndex % totalTxsInWindow) < neededHotTxs) {
            // DEPENDENT: pick from shared hot key pool — creates read-write conflicts
            // Use modulo for key selection to ensure perfectly even distribution across hotKeys
            const hotKeyIndex = this.txIndex % this.hotKeys;
            key = `hot_${hotKeyIndex}`;
        } else {
            // INDEPENDENT: unique key — no conflicts possible
            key = `uniq_${this.workerIndex}_${this.txIndex}`;
        }

        // Determine if this transaction will be cross-shard based on pcross probability
        const isCrossShard = Math.random() < this.pcross;

        // Build the secondary shard list
        const secondaryShards = [];
        if (isCrossShard) {
            const available = this.shards.filter((_, idx) => idx !== primaryShardIndex);
            // Random number of secondary shards: 1 to all remaining (2 to 7 total shards)
            const count = 1 + Math.floor(Math.random() * available.length);
            // Shuffle and pick 'count' distinct shards
            for (let i = available.length - 1; i > 0; i--) {
                const j = Math.floor(Math.random() * (i + 1));
                [available[i], available[j]] = [available[j], available[i]];
            }
            for (let i = 0; i < count; i++) {
                secondaryShards.push(available[i]);
            }
        }

        const contractArguments = [key, 'value'];

        // Append each secondary shard as a separate argument
        if (secondaryShards.length > 0) {
            for (const shard of secondaryShards) {
                contractArguments.push(shard);
            }
        } else {
            contractArguments.push(''); // No cross-shard
        }

        const args = {
            contractId: primaryShard,
            contractFunction: 'invoke',
            contractArguments: contractArguments,
            readOnly: false
        };

        return this.sutAdapter.sendRequests(args);
    }
}

function createWorkloadModule() {
    return new CrossShardLoad();
}

module.exports.createWorkloadModule = createWorkloadModule;
