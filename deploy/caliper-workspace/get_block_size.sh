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
    echo "Extracting block sizes directly from docker orderer.example.com..."
    LOGS=$(docker logs orderer.example.com 2>&1 | grep "PHYSICAL SIZE =")
else
    LOG_FILE=$1
    if [ ! -f "$LOG_FILE" ]; then
        echo "Error: File $LOG_FILE not found."
        exit 1
    fi
    echo "Extracting block sizes from $LOG_FILE..."
    LOGS=$(cat "$LOG_FILE" | grep "PHYSICAL SIZE =")
fi

if [ -z "$LOGS" ]; then
    echo "No valid PHYSICAL SIZE logs found. Did you deploy the recompiled orderer?"
    exit 1
fi

# We know the user runs 5 rounds: 1000, 2000, 3000, 4000, and 5000 txns
# Depending on block cutter settings (often 500 txs/block or 2 sec timeout),
# we can map the sequential blocks into these 5 bins by counting the transactions they represent
# OR simpler: Since Caliper sends transactions sequentially per round with gaps,
# we can chunk the logs based on timestamp gaps or simply just report EVERY block 
# and let the user see the exact progression.

# Let's print the actual sequence of blocks and compute a moving average or grouped average.
# It is safest to just output the raw sizes of all blocks so the user can see how they grew
# with the transaction volume.

echo -e "\nBlock Number | Size (Bytes) | Size (KB)"
echo "----------------------------------------"

total_bytes=0
total_blocks=0

while IFS= read -r line; do
    byte_size=$(echo "$line" | grep -o 'PHYSICAL SIZE = [0-9]*' | awk '{print $4}')
    block_num=$(echo "$line" | grep -o 'Writing block \[[0-9]*\]' | tr -d 'Writing block []')
    
    if [ ! -z "$byte_size" ]; then
        kb_size=$(echo "scale=2; $byte_size / 1024" | bc)
        printf "Block %-6s | %-12s | %-8s KB\n" "$block_num" "$byte_size" "$kb_size"
        total_bytes=$((total_bytes + byte_size))
        total_blocks=$((total_blocks + 1))
    fi
done <<< "$LOGS"

if [ "$total_blocks" -gt 0 ]; then
    avg_block_bytes=$(echo "scale=2; $total_bytes / $total_blocks" | bc)
    avg_block_kb=$(echo "scale=2; $avg_block_bytes / 1024" | bc)
    
    echo "----------------------------------------"
    echo "Total blocks analyzed: $total_blocks"
    echo "Average block size: $avg_block_bytes bytes/block (${avg_block_kb} KB/block)"
    echo "========================================="
fi
