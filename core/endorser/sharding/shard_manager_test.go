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
