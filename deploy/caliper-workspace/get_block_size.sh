#!/bin/bash

# Extract block sizes in bytes from orderer logs and bin them by experiment (tx volumes)
# Usage: ./get_block_size.sh <path_to_orderer_log_file>

if [ -z "$1" ]; then
    echo "Usage: ./get_block_size.sh <path_to_orderer_log_file>"
    echo "Example: ./get_block_size.sh orderer.example.com.log"
    echo "Or if using docker directly: ./get_block_size.sh docker"
    exit 1
fi

if [ "$1" == "docker" ]; then
    echo -e "Extracting block sizes directly from docker orderer.example.com...\n"
    LOGS=$(docker logs orderer.example.com 2>&1 | grep "PHYSICAL SIZE =" | awk -F "PHYSICAL SIZE = " '{print $2}' | awk '{print $1}')
else
    LOG_FILE=$1
    if [ ! -f "$LOG_FILE" ]; then
        echo "Error: File $LOG_FILE not found."
        exit 1
    fi
    echo -e "Extracting block sizes from $LOG_FILE...\n"
    LOGS=$(cat "$LOG_FILE" | grep "PHYSICAL SIZE =" | awk -F "PHYSICAL SIZE = " '{print $2}' | awk '{print $1}')
fi

if [ -z "$LOGS" ]; then
    echo "No valid PHYSICAL SIZE logs found. Did you deploy the recompiled orderer?"
    exit 1
fi

bytes_array=($LOGS)

echo "=================================================="
echo "          EXPERIMENT ROUND 1 (1000 TXs)"
echo "--------------------------------------------------"

round=1
target_txs=$((round * 1000))

current_round_bytes=0
current_round_blocks=0
current_round_tx_count=0

setup_blocks=0
total_payload_bytes=0
total_payload_blocks=0

for i in "${!bytes_array[@]}"; do
    byte_size=${bytes_array[$i]}
    block_num=$((i + 1))
    
    # Ignore initial setup/genesis/channel blocks which are < 50KB
    if [[ $block_num -lt 15 && $byte_size -lt 50000 ]]; then
        setup_blocks=$((setup_blocks + 1))
        continue
    fi
    
    # Ignore empty or trailing config blocks which are tiny
    if [[ $byte_size -lt 50000 ]]; then
        continue
    fi

    # Based on the data you provided, a 500-tx block is roughly ~510,000 bytes.
    # Therefore, 1 transaction is approximately ~1020 bytes.
    est_txs=$((byte_size / 1020))
    if [ $est_txs -eq 0 ]; then est_txs=1; fi
    
    current_round_bytes=$((current_round_bytes + byte_size))
    current_round_blocks=$((current_round_blocks + 1))
    current_round_tx_count=$((current_round_tx_count + est_txs))
    
    total_payload_bytes=$((total_payload_bytes + byte_size))
    total_payload_blocks=$((total_payload_blocks + 1))
    
    kb_size=$(echo "scale=2; $byte_size / 1024" | bc)
    printf "Block %-4s | %-8s Bytes | %-8s KB | (est. %s txs)\n" "$block_num" "$byte_size" "$kb_size" "$est_txs"
    
    # If the accumulated payload in this group exceeds the workload target (1000, 2000, etc),
    # then this round's payload cluster has finished!
    if [[ $current_round_tx_count -ge $target_txs ]]; then
        avg_round_bytes=$((current_round_bytes / current_round_blocks))
        avg_round_kb=$(echo "scale=2; $avg_round_bytes / 1024" | bc)
        
        echo "--------------------------------------------------"
        echo ">>> Round $round Snapshot: $current_round_blocks blocks used"
        echo ">>> Avg Block Size for Round: $avg_round_bytes Bytes ($avg_round_kb KB)"
        echo "=================================================="
        
        if [[ $round -lt 5 ]]; then
            echo ""
            round=$((round + 1))
            target_txs=$((round * 1000))
            echo "=================================================="
            echo "          EXPERIMENT ROUND $round ($target_txs TXs)"
            echo "--------------------------------------------------"
        fi
        
        current_round_tx_count=0
        current_round_blocks=0
        current_round_bytes=0
    fi
done

if [[ $current_round_blocks -gt 0 ]]; then
     avg_round_bytes=$((current_round_bytes / current_round_blocks))
     avg_round_kb=$(echo "scale=2; $avg_round_bytes / 1024" | bc)
     echo "--------------------------------------------------"
     echo ">>> Trailing Blocks Snapshot: $current_round_blocks blocks used"
     echo ">>> Avg Block Size: $avg_round_bytes Bytes ($avg_round_kb KB)"
     echo "=================================================="
fi

if [[ $total_payload_blocks -gt 0 ]]; then
    echo ""
    avg_total_bytes=$((total_payload_bytes / total_payload_blocks))
    avg_total_kb=$(echo "scale=2; $avg_total_bytes / 1024" | bc)
    echo "Total Setup/Config Blocks Ignored: $setup_blocks"
    echo "OVERALL Average Block Size (All Rounds): $avg_total_bytes Bytes ($avg_total_kb KB)"
else
    echo "No valid blocks analyzed."
fi
