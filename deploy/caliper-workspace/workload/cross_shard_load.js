'use strict';

const { WorkloadModuleBase } = require('@hyperledger/caliper-core');

/**
 * Workload module for simulating cross-shard transactions with configurable
 * key contention (hot keys) and cross-shard probability.
 *
 * Parameters:
 *   pcross   - Probability [0,1] that a transaction is cross-shard (default: 0.10)
 *   hotKeys  - Number of shared keys in the pool (default: 10). Lower = more contention
 *              and deeper DAG levels. Higher = less contention, more parallelism.
 */
class CrossShardLoad extends WorkloadModuleBase {

    async initializeWorkloadModule(workerIndex, totalWorkers, roundIndex, roundArguments, sutAdapter, sutContext) {
        await super.initializeWorkloadModule(workerIndex, totalWorkers, roundIndex, roundArguments, sutAdapter, sutContext);

        this.txIndex = 0;
        this.pcross = this.roundArguments.pcross !== undefined ? this.roundArguments.pcross : 0.10;
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

        // Pick a HOT KEY from the shared pool — this creates contention and real dependencies
        const hotKeyIndex = Math.floor(Math.random() * this.hotKeys);
        const key = `hot_${hotKeyIndex}`;

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
