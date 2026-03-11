#!/bin/bash

# Extract block sizes in bytes from orderer logs
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
echo "          EXPERIMENT ROUND (5000 TXs)"
echo "--------------------------------------------------"

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
    
    # Ignore trailing empty/timer cut blocks
    if [[ $byte_size -lt 50000 ]]; then
        continue
    fi
    
    total_payload_bytes=$((total_payload_bytes + byte_size))
    total_payload_blocks=$((total_payload_blocks + 1))
    
    kb_size=$(echo "scale=2; $byte_size / 1024" | bc)
    printf "Block %-4s | %-8s Bytes | %-8s KB\n" "$block_num" "$byte_size" "$kb_size"
done

if [[ $total_payload_blocks -gt 0 ]]; then
    echo "=================================================="
    avg_total_bytes=$((total_payload_bytes / total_payload_blocks))
    avg_total_kb=$(echo "scale=2; $avg_total_bytes / 1024" | bc)
    echo "Total Payload Blocks: $total_payload_blocks (Ignored Setup Blocks: $setup_blocks)"
    echo "OVERALL Average Block Size: $avg_total_bytes Bytes ($avg_total_kb KB)"
    echo "=================================================="
else
    echo "No valid blocks analyzed."
fi
