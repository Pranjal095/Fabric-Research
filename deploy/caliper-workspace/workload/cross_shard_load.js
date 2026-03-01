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
        let secondaryShard = null;
        const isCrossShard = Math.random() < this.pcross;

        if (isCrossShard) {
            // Select a different shard for cross-chaincode mock state injection
            let secondaryShardIndex = Math.floor(Math.random() * this.shards.length);
            while (secondaryShardIndex === primaryShardIndex) {
                secondaryShardIndex = Math.floor(Math.random() * this.shards.length);
            }
            secondaryShard = this.shards[secondaryShardIndex];
        }

        const args = {
            contractId: primaryShard,
            contractFunction: 'invoke',
            contractArguments: [
                `key_${this.workerIndex}_${this.txIndex}`,
                'value',
                secondaryShard ? secondaryShard : '' // Send secondary shard context if triggered
            ],
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
