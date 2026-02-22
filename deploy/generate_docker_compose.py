#!/usr/bin/env python3
import argparse
import yaml
import os

def generate_compose(num_peers, server_id, start_port=7051, couch_start_port=5984):
    services = {}
    volumes = {}
    
    for i in range(1, num_peers + 1):
        global_peer_id = (server_id - 1) * num_peers + i
        peer_name = f"peer{global_peer_id}.org1.example.com"
        couch_name = f"couchdb{global_peer_id}"
        
        peer_port = start_port + (i - 1) * 1000
        couch_port = couch_start_port + (i - 1) * 1000
        chaincode_port = peer_port + 1
        
        # CouchDB Service
        services[couch_name] = {
            "image": "couchdb:3.1.1",
            "environment": [
                "COUCHDB_USER=admin",
                "COUCHDB_PASSWORD=adminpw"
            ],
            "ports": [f"{couch_port}:5984"],
            "networks": ["fabric_test"]
        }
        
        # Peer Service
        services[peer_name] = {
            "container_name": peer_name,
            "image": "hyperledger/fabric-peer:latest",
            "environment": [
                "CORE_VM_ENDPOINT=unix:///host/var/run/docker.sock",
                "CORE_VM_DOCKER_HOSTCONFIG_NETWORKMODE=fabric_test",
                f"CORE_PEER_ID={peer_name}",
                f"CORE_PEER_ADDRESS={peer_name}:{peer_port}",
                f"CORE_PEER_LISTENADDRESS=0.0.0.0:{peer_port}",
                f"CORE_PEER_CHAINCODELISTENADDRESS=0.0.0.0:{chaincode_port}",
                "CORE_PEER_GOSSIP_BOOTSTRAP=peer1.org1.example.com:7051",
                f"CORE_PEER_GOSSIP_EXTERNALENDPOINT={peer_name}:{peer_port}",
                "CORE_PEER_LOCALMSPID=Org1MSP",
                "CORE_LEDGER_STATE_STATEDATABASE=CouchDB",
                f"CORE_LEDGER_STATE_COUCHDBCONFIG_COUCHDBADDRESS={couch_name}:5984",
                "CORE_LEDGER_STATE_COUCHDBCONFIG_USERNAME=admin",
                "CORE_LEDGER_STATE_COUCHDBCONFIG_PASSWORD=adminpw",
                "EXPERIMENTAL_SHARDING_ENABLED=true",
            ],
            "volumes": [
                "/var/run/docker.sock:/host/var/run/docker.sock",
                f"peer{global_peer_id}.org1.example.com:/var/hyperledger/production",
                "../build/bin/peer:/usr/local/bin/peer:ro", # Map the custom built binary
                "./sharding.json:/opt/gopath/src/github.com/hyperledger/fabric/peer/sharding.json:ro", # Map the cluster config
            ],
            "working_dir": "/opt/gopath/src/github.com/hyperledger/fabric/peer",
            "command": "peer node start",
            "ports": [
                f"{peer_port}:{peer_port}",
                f"{chaincode_port}:{chaincode_port}"
            ],
            "depends_on": [couch_name],
            "networks": ["fabric_test"]
        }
        
        volumes[peer_name] = None
        
    compose = {
        "version": "3.7",
        "networks": {
            "fabric_test": {
                "name": "fabric_test"
            }
        },
        "volumes": volumes,
        "services": services
    }
    
    with open(f"docker-compose-server{server_id}.yaml", "w") as f:
        yaml.dump(compose, f, sort_keys=False)
        
    print(f"Generated docker-compose-server{server_id}.yaml for {num_peers} peers.")

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Generate Docker Compose for Fabric Peers")
    parser.add_argument("--peers", type=int, default=3, help="Number of peers to generate")
    parser.add_argument("--server", type=int, default=1, help="Server ID (1, 2, or 3)")
    args = parser.parse_args()
    
    generate_compose(args.peers, args.server)
