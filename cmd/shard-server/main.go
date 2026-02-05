package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/core/endorser/sharding"
)

var logger = flogging.MustGetLogger("shard-server")

// ClusterConfig represents the cluster topology
type ClusterConfig struct {
	Peers map[uint64]string `json:"peers"` // ID -> "IP:Port"
}

func main() {
	var (
		nodeID     uint64
		configFile string
		shardID    string
	)

	flag.Uint64Var(&nodeID, "id", 0, "Node ID (must be > 0)")
	flag.StringVar(&configFile, "config", "cluster.json", "Path to cluster config file")
	flag.StringVar(&shardID, "shard", "my-shard", "Shard ID/Contract Name")
	flag.Parse()

	if nodeID == 0 {
		logger.Error("Node ID must be greater than 0")
		os.Exit(1)
	}

	// Load config
	configData, err := ioutil.ReadFile(configFile)
	if err != nil {
		logger.Errorf("Failed to read config file: %v", err)
		os.Exit(1)
	}

	var clusterConfig ClusterConfig
	if err := json.Unmarshal(configData, &clusterConfig); err != nil {
		logger.Errorf("Failed to parse config file: %v", err)
		os.Exit(1)
	}

	myAddr, ok := clusterConfig.Peers[nodeID]
	if !ok {
		logger.Errorf("Node ID %d not found in config", nodeID)
		os.Exit(1)
	}

	logger.Infof("Starting Shard Node %d at %s", nodeID, myAddr)

    // Generate dummy nodes list for ShardLeader config
    dummyNodes := make([]string, len(clusterConfig.Peers))
    for i := range dummyNodes {
        dummyNodes[i] = fmt.Sprintf("node%d", i+1)
    }

	cfg := sharding.ShardConfig{
		ShardID:      shardID,
		ReplicaNodes: dummyNodes, // This implies IDs 1..N
		ReplicaID:    nodeID,
	}

	leader, err := sharding.NewShardLeader(cfg, 300*time.Millisecond, 50)
	if err != nil {
		logger.Errorf("Failed to create shard leader: %v", err)
		os.Exit(1)
	}

	// Create Transport
	peerConfig := sharding.PeerConfig(clusterConfig.Peers)
	transport := sharding.NewTransport(nodeID, myAddr, peerConfig, leader)

	if err := transport.Start(); err != nil {
		logger.Errorf("Failed to start transport: %v", err)
		os.Exit(1)
	}

	logger.Info("Shard Server Started Successfully")

	// Block until signal
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	logger.Info("Shutting down...")
	transport.Stop()
	leader.Stop()
}
