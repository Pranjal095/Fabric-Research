package sharding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// Global tracker to ensure HTTP server runs only once per peer process
var globalHTTPStarted bool

// StartHTTPServer starts a REST API for remote shard proposal coordination.
// It offsets the default peer port by 30000 (e.g., 7051 -> 37051).
func (sm *ShardManager) StartHTTPServer(myAddr string) {
	host, portStr, err := net.SplitHostPort(myAddr)
	if err != nil {
		logger.Errorf("Failed to split host/port for HTTP Server: %v", err)
		return
	}
	_ = host // unused locally for binding
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	bindAddr := fmt.Sprintf("0.0.0.0:%d", port+30000)

	mux := http.NewServeMux()
	mux.HandleFunc("/propose", func(w http.ResponseWriter, r *http.Request) {
		var req PrepareRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		shard, err := sm.GetOrCreateShard(req.ShardID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		commitC := shard.Subscribe(req.TxID)
		defer shard.Unsubscribe(req.TxID, commitC)

		if !shard.HasProof(req.TxID) {
			select {
			case shard.ProposeC() <- &req:
			default:
				http.Error(w, "propose channel full", http.StatusServiceUnavailable)
				return
			}
		}

		select {
		case proof := <-commitC:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(proof)
		case <-time.After(30 * time.Second): // DefaultPrepareTimeout
			http.Error(w, "timeout waiting for local shard proof", http.StatusGatewayTimeout)
		}
	})

	go func() {
		logger.Infof("Starting Shard Remote REST API at %s", bindAddr)
		if err := http.ListenAndServe(bindAddr, mux); err != nil {
			logger.Errorf("REST API Failed: %v", err)
		}
	}()
}

// RequestRemoteProof requests a dependency proof from an actual replica over HTTP
func (sm *ShardManager) RequestRemoteProof(shardID string, req *PrepareRequest) (*PrepareProof, error) {
	var targetAddr string
	if externalConfig, err := loadShardingConfig("sharding.json"); err == nil {
		if replicas, ok := externalConfig[shardID]; ok && len(replicas) > 0 {
			targetAddr = replicas[0] // Pick the first replica in the list to handle the dependency coord
		}
	}

	if targetAddr == "" {
		return nil, fmt.Errorf("no replicas found for shard %s in sharding config", shardID)
	}

	host, portStr, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to split host/port for target %s: %v", targetAddr, err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	url := fmt.Sprintf("http://%s:%d/propose", host, port+30000)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("remote HTTP error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("remote error: %s (status %d)", resp.Status, resp.StatusCode)
	}

	var proof PrepareProof
	if err := json.NewDecoder(resp.Body).Decode(&proof); err != nil {
		return nil, fmt.Errorf("failed to decode proof: %v", err)
	}

	return &proof, nil
}

// IsReplica checks if the current peer's address matches any of the ReplicaNodes for the given Contract/Shard
func (sm *ShardManager) IsReplica(shardID string) bool {
	myAddr := os.Getenv("CORE_PEER_ADDRESS")
	if myAddr == "" {
		myAddr = "localhost:7051"
	}
	if externalConfig, err := loadShardingConfig("sharding.json"); err == nil {
		if replicas, ok := externalConfig[shardID]; ok {
			for _, nodeAddr := range replicas {
				if nodeAddr == myAddr {
					return true
				}
			}
		}
	}
	return false
}
