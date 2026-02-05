#!/usr/bin/env python3
import json
import sys

def main():
    print("Generating cluster.json for 15 nodes on 3 servers.")
    
    server1 = input("Enter IP for Server 1 (Nodes 1-5) [localhost]: ") or "127.0.0.1"
    server2 = input("Enter IP for Server 2 (Nodes 6-10) [localhost]: ") or "127.0.0.1"
    server3 = input("Enter IP for Server 3 (Nodes 11-15) [localhost]: ") or "127.0.0.1"
    
    base_port = 7000
    peers = {}
    
    # Nodes 1-5 on Server 1
    for i in range(1, 6):
        peers[i] = f"{server1}:{base_port + i}"
        
    # Nodes 6-10 on Server 2
    for i in range(6, 11):
        peers[i] = f"{server2}:{base_port + i - 5}" # Reusing ports 7001-7005? Or unique? 
        # Better to likely use unique ports if all localhost, but on different servers reuse is fine.
        # However, to be safe against port conflicts if testing on one machine, let's increment.
        # But for separate servers, ports 7001-7005 is standard. 
        # Let's assume distinct IPs. If IPs are same, we must use distinct ports.
        
        if server1 == server2 == server3:
             peers[i] = f"{server2}:{base_port + i}"
        else:
             peers[i] = f"{server2}:{base_port + (i-5)}" # 7001..7005
             
    # Nodes 11-15 on Server 3
    for i in range(11, 16):
        if server1 == server2 == server3:
             peers[i] = f"{server3}:{base_port + i}"
        else:
             peers[i] = f"{server3}:{base_port + (i-10)}" # 7001..7005

    config = {"peers": peers}
    
    with open("cluster.json", "w") as f:
        json.dump(config, f, indent=4)
        
    print(f"Generated cluster.json with {len(peers)} nodes.")
    print("Copy this file to all servers.")

if __name__ == "__main__":
    main()
