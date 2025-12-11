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