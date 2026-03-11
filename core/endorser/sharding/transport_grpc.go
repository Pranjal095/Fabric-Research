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
	"google.golang.org/grpc/metadata"
)

// PeerConfig maps NodeID to Address (host:port)
type PeerConfig map[uint64]string

// Transport manages network communication for a shard node
type Transport struct {
	protos.UnimplementedShardCommunicationServer
	nodeID     uint64
	address    string
	peers      PeerConfig
	leaders    map[string]*ShardLeader
	leadersMu  sync.RWMutex
	grpcServer *grpc.Server
	clients    map[uint64]protos.ShardCommunicationClient
	clientConn map[uint64]*grpc.ClientConn
	mu         sync.RWMutex
	stopC      chan struct{}
}

// NewTransport creates a new gRPC transport
func NewTransport(nodeID uint64, address string, peers PeerConfig) *Transport {
	return &Transport{
		nodeID:     nodeID,
		address:    address,
		peers:      peers,
		leaders:    make(map[string]*ShardLeader),
		clients:    make(map[uint64]protos.ShardCommunicationClient),
		clientConn: make(map[uint64]*grpc.ClientConn),
		stopC:      make(chan struct{}),
	}
}

// RegisterShard registers a shard leader with the transport
func (t *Transport) RegisterShard(shardID string, leader *ShardLeader) {
	t.leadersMu.Lock()
	t.leaders[shardID] = leader
	t.leadersMu.Unlock()
	go t.consumeMessages(shardID, leader)
}

// parseAndOffsetPort adds an offset to the port in a host:port string
func parseAndOffsetPort(addr string, offset int) (string, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port+offset)), nil
}

// Start starts the gRPC server and message consumer
func (t *Transport) Start() error {
	// Offset port by 20000 to avoid collision with Fabric Endorser
	offsetAddr, err := parseAndOffsetPort(t.address, 20000)
	if err != nil {
		return fmt.Errorf("failed to offset port for address %s: %v", t.address, err)
	}

	// Parse the port from t.address to bind to 0.0.0.0, because the container
	// doesn't own the host's routable IP
	_, port, err := net.SplitHostPort(offsetAddr)
	if err != nil {
		return fmt.Errorf("failed to parse transport address %s: %v", offsetAddr, err)
	}

	bindAddr := fmt.Sprintf("0.0.0.0:%s", port)
	lis, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", bindAddr, err)
	}

	t.grpcServer = grpc.NewServer()
	protos.RegisterShardCommunicationServer(t.grpcServer, t)

	// Start server
	go func() {
		if err := t.grpcServer.Serve(lis); err != nil {
			logger.Errorf("gRPC server error: %v", err)
		}
	}()

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
	md, ok := metadata.FromIncomingContext(ctx)
	shardID := ""
	if ok && len(md["shard-id"]) > 0 {
		shardID = md["shard-id"][0]
	}

	if shardID == "" {
		return &protos.StepResponse{Success: false, Error: "missing shard-id in metadata"}, nil
	}

	t.leadersMu.RLock()
	leader, exists := t.leaders[shardID]
	t.leadersMu.RUnlock()

	if !exists {
		return &protos.StepResponse{Success: false, Error: fmt.Sprintf("shard %s not found on this node", shardID)}, nil
	}

	var msg raftpb.Message
	if err := msg.Unmarshal(req.Data); err != nil {
		return &protos.StepResponse{Success: false, Error: err.Error()}, nil
	}

	if err := leader.Step(ctx, msg); err != nil {
		return &protos.StepResponse{Success: false, Error: err.Error()}, nil
	}

	return &protos.StepResponse{Success: true}, nil
}

// consumeMessages reads outgoing messages from ShardLeader and sends them
func (t *Transport) consumeMessages(shardID string, leader *ShardLeader) {
	for {
		select {
		case msgs := <-leader.MessagesC():
			for _, msg := range msgs {
				go t.send(shardID, msg)
			}
		case <-t.stopC:
			return
		}
	}
}

// send sends a single Raft message to a peer
func (t *Transport) send(shardID string, msg raftpb.Message) {
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

	// Use an aggressive 500ms timeout for internal Raft routing since heartbeat ticks
	// run every 100ms. If network sockets silently drop packets (like Hairpin NAT connection tracking bugs),
	// waiting 15s will deadlock the HTTP/2 grpc.ClientConn `MaxConcurrentStreams=100` limits for the peer.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ctx = metadata.AppendToOutgoingContext(ctx, "shard-id", shardID)

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

	dialAddr, err := parseAndOffsetPort(addr, 20000)
	if err != nil {
		return nil, fmt.Errorf("failed to offset port for peer %d address %s: %v", nodeID, addr, err)
	}

	// Connect
	conn, err := grpc.Dial(dialAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	client = protos.NewShardCommunicationClient(conn)
	t.clients[nodeID] = client
	t.clientConn[nodeID] = conn

	return client, nil
}
