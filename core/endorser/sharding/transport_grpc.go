package sharding

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/hyperledger/fabric/core/endorser/sharding/protos"
	"go.etcd.io/etcd/raft/v3/raftpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// PeerConfig maps NodeID to Address (host:port)
type PeerConfig map[uint64]string

// Transport manages network communication for a shard node
type Transport struct {
	protos.UnimplementedShardCommunicationServer
	nodeID     uint64
	address    string
	peers      PeerConfig
	leader     *ShardLeader
	grpcServer *grpc.Server
	clients    map[uint64]protos.ShardCommunicationClient
	clientConn map[uint64]*grpc.ClientConn
	mu         sync.RWMutex
	stopC      chan struct{}
}

// NewTransport creates a new gRPC transport
func NewTransport(nodeID uint64, address string, peers PeerConfig, leader *ShardLeader) *Transport {
	return &Transport{
		nodeID:     nodeID,
		address:    address,
		peers:      peers,
		leader:     leader,
		clients:    make(map[uint64]protos.ShardCommunicationClient),
		clientConn: make(map[uint64]*grpc.ClientConn),
		stopC:      make(chan struct{}),
	}
}

// Start starts the gRPC server and message consumer
func (t *Transport) Start() error {
	lis, err := net.Listen("tcp", t.address)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	t.grpcServer = grpc.NewServer()
	protos.RegisterShardCommunicationServer(t.grpcServer, t)

	// Start server
	go func() {
		if err := t.grpcServer.Serve(lis); err != nil {
			logger.Errorf("gRPC server error: %v", err)
		}
	}()

	// Start message consumer
	go t.consumeMessages()

	return nil
}

// Stop stops the transport
func (t *Transport) Stop() {
	close(t.stopC)
	if t.grpcServer != nil {
		t.grpcServer.Stop()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, conn := range t.clientConn {
		conn.Close()
	}
}

// Step receives a message from a peer (gRPC handler)
func (t *Transport) Step(ctx context.Context, req *protos.RaftMessageProto) (*protos.StepResponse, error) {
	var msg raftpb.Message
	if err := msg.Unmarshal(req.Data); err != nil {
		return &protos.StepResponse{Success: false, Error: err.Error()}, nil
	}

	if err := t.leader.Step(ctx, msg); err != nil {
		return &protos.StepResponse{Success: false, Error: err.Error()}, nil
	}

	return &protos.StepResponse{Success: true}, nil
}

// consumeMessages reads outgoing messages from ShardLeader and sends them
func (t *Transport) consumeMessages() {
	for {
		select {
		case msgs := <-t.leader.MessagesC():
			for _, msg := range msgs {
				go t.send(msg)
			}
		case <-t.stopC:
			return
		}
	}
}

// send sends a single Raft message to a peer
func (t *Transport) send(msg raftpb.Message) {
	client, err := t.getClient(msg.To)
	if err != nil {
		logger.Errorf("Failed to get client for node %d: %v", msg.To, err)
		return
	}

	data, err := msg.Marshal()
	if err != nil {
		logger.Errorf("Failed to marshal raft message: %v", err)
		return
	}

	req := &protos.RaftMessageProto{
		Data: data,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.Step(ctx, req)
	if err != nil {
		logger.Warnf("Failed to send message to node %d: %v", msg.To, err)
	}
}

// getClient returns or creates a gRPC client for a node
func (t *Transport) getClient(nodeID uint64) (protos.ShardCommunicationClient, error) {
	t.mu.RLock()
	client, exists := t.clients[nodeID]
	t.mu.RUnlock()
	if exists {
		return client, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Double check
	if client, exists := t.clients[nodeID]; exists {
		return client, nil
	}

	addr, ok := t.peers[nodeID]
	if !ok {
		return nil, fmt.Errorf("unknown peer %d", nodeID)
	}

	// Connect
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	client = protos.NewShardCommunicationClient(conn)
	t.clients[nodeID] = client
	t.clientConn[nodeID] = conn

	return client, nil
}
