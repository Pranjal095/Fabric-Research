'use strict';

const { WorkloadModuleBase } = require('@hyperledger/caliper-core');

/**
 * Workload module for simulating cross-shard transactions based on a mathematical pcross probability.
 */
class CrossShardLoad extends WorkloadModuleBase {

    /**
     * Initializes the workload module.
     * @param {number} workerIndex The 0-based index of the worker instantiating the workload module.
     * @param {number} totalWorkers The total number of workers participating in the round.
     * @param {number} roundIndex The 0-based index of the currently executing round.
     * @param {Object} roundArguments The user-provided arguments for the round from the benchmark configuration file.
     * @param {BlockchainInterface} sutAdapter The adapter of the underlying SUT.
     * @param {Object} sutContext The custom context object provided by the SUT adapter.
     */
    async initializeWorkloadModule(workerIndex, totalWorkers, roundIndex, roundArguments, sutAdapter, sutContext) {
        await super.initializeWorkloadModule(workerIndex, totalWorkers, roundIndex, roundArguments, sutAdapter, sutContext);

        this.txIndex = 0;
        this.pcross = this.roundArguments.pcross !== undefined ? this.roundArguments.pcross : 0.10;

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

    /**
     * Assemble TXs for the round.
     * @return {Promise<TxStatus[]>}
     */
    async submitTransaction() {
        this.txIndex++;

        // Select the primary target shard randomly
        const primaryShardIndex = Math.floor(Math.random() * this.shards.length);
        const primaryShard = this.shards[primaryShardIndex];

        // Determine if this transaction will be cross-shard based on pcross probability
        const isCrossShard = Math.random() < this.pcross;

        // Build the secondary shard list
        const secondaryShards = [];
        if (isCrossShard) {
            const available = this.shards.filter((_, idx) => idx !== primaryShardIndex);
            // Random number of secondary shards: 1 to all remaining (so 2 to 7 total shards)
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

        const contractArguments = [
            `key_${this.workerIndex}_${this.txIndex}`,
            'value',
        ];

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

/**
 * Create a new instance of the workload module.
 * @return {WorkloadModuleInterface}
 */
function createWorkloadModule() {
    return new CrossShardLoad();
}

module.exports.createWorkloadModule = createWorkloadModule;
