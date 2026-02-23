#!/usr/bin/env python3
import argparse
import yaml
import os

def generate_compose(num_peers, server_id, start_peer=0, start_port=7051, couch_start_port=5984):
    services = {}
    volumes = {}
    
    for i in range(num_peers):
        global_peer_id = start_peer + i
        peer_name = f"peer{global_peer_id}.org1.example.com"
        couch_name = f"couchdb{global_peer_id}"
        peer_port = start_port + i * 1000
        couch_port = couch_start_port + i * 1000
        chaincode_port = peer_port + 1
        
        # Add Orderer on Server 1
        if server_id == 1 and i == 0:
            services["orderer.example.com"] = {
                "container_name": "orderer.example.com",
                "image": "hyperledger/fabric-orderer:latest",
                "environment": [
                    "FABRIC_LOGGING_SPEC=INFO",
                    "ORDERER_GENERAL_LISTENADDRESS=0.0.0.0",
                    "ORDERER_GENERAL_LISTENPORT=7050",
                    "ORDERER_GENERAL_LOCALMSPID=OrdererMSP",
                    "ORDERER_GENERAL_LOCALMSPDIR=/var/hyperledger/orderer/msp",
                    "ORDERER_GENERAL_TLS_ENABLED=false",
                    "ORDERER_GENERAL_BOOTSTRAPMETHOD=none",
                    "ORDERER_CHANNELPARTICIPATION_ENABLED=true",
                    "ORDERER_ADMIN_TLS_ENABLED=false",
                    "ORDERER_ADMIN_LISTENADDRESS=0.0.0.0:7053"
                ],
                "working_dir": "/opt/gopath/src/github.com/hyperledger/fabric",
                "command": "orderer",
                "volumes": [
                    "orderer.example.com:/var/hyperledger/production/orderer",
                    "../build/bin/orderer:/usr/local/bin/orderer:ro",
                    "./crypto-config/ordererOrganizations/example.com/orderers/orderer.example.com/msp:/var/hyperledger/orderer/msp",
                    "./crypto-config/ordererOrganizations/example.com/orderers/orderer.example.com/tls:/var/hyperledger/orderer/tls"
                ],
                "ports": [
                    "7050:7050",
                    "7053:7053"
                ],
                "networks": ["fabric_test"]
            }
            volumes["orderer.example.com"] = None
        
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
                "CORE_PEER_GOSSIP_BOOTSTRAP=peer0.org1.example.com:7051",
                f"CORE_PEER_GOSSIP_EXTERNALENDPOINT={peer_name}:{peer_port}",
                "CORE_PEER_LOCALMSPID=Org1MSP",
                "CORE_LEDGER_STATE_STATEDATABASE=CouchDB",
                f"CORE_LEDGER_STATE_COUCHDBCONFIG_COUCHDBADDRESS={couch_name}:5984",
                "CORE_LEDGER_STATE_COUCHDBCONFIG_USERNAME=admin",
                "CORE_LEDGER_STATE_COUCHDBCONFIG_PASSWORD=adminpw",
                "EXPERIMENTAL_SHARDING_ENABLED=true",
                "CORE_PEER_TLS_ENABLED=false",
                "CORE_PEER_MSPCONFIGPATH=/etc/hyperledger/fabric/msp",
            ],
            "volumes": [
                "/var/run/docker.sock:/host/var/run/docker.sock",
                f"peer{global_peer_id}.org1.example.com:/var/hyperledger/production",
                "../build/bin/peer:/usr/local/bin/peer:ro", # Map the custom built binary
                "./sharding.json:/opt/gopath/src/github.com/hyperledger/fabric/peer/sharding.json:ro", # Map the cluster config
                f"./crypto-config/peerOrganizations/org1.example.com/peers/{peer_name}/msp:/etc/hyperledger/fabric/msp",
                f"./crypto-config/peerOrganizations/org1.example.com/peers/{peer_name}/tls:/etc/hyperledger/fabric/tls",
                "../../sampleconfig/core.yaml:/etc/hyperledger/fabric/core.yaml:ro",
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
    parser.add_argument("--start-peer", type=int, default=0, help="Starting global peer ID index")
    args = parser.parse_args()
    
    generate_compose(args.peers, args.server, args.start_peer)
