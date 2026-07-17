#!/bin/bash

#############################################
# CERP Daily Data Generation Script
# This script runs via cron to generate
# daily trial data automatically
#############################################

# Configuration
API_URL="http://localhost:7002/activation-api/trigger-daily-data"
LOG_FILE="/var/log/cerp-daily-data.log"

# Add your organization IDs here (one per line)
ORG_IDS=(
    "org-2026-81"
    "org-2026-80"
)

# Create log file if it doesn't exist
touch $LOG_FILE 2>/dev/null || LOG_FILE="$HOME/cerp-daily-data.log"

# Log separator
echo "========================================" >> $LOG_FILE
echo "Daily Data Generation Started" >> $LOG_FILE
echo "Date: $(date '+%Y-%m-%d %H:%M:%S')" >> $LOG_FILE
echo "========================================" >> $LOG_FILE

# Loop through all organizations
for ORG_ID in "${ORG_IDS[@]}"
do
    echo "" >> $LOG_FILE
    echo "Processing Organization: $ORG_ID" >> $LOG_FILE
    echo "Time: $(date '+%H:%M:%S')" >> $LOG_FILE
    
    # Make API call
    response=$(curl -s -w "\nHTTP_STATUS:%{http_code}" -X POST "${API_URL}?orgId=${ORG_ID}" \
        -H "Content-Type: application/json" \
        --max-time 30)
    
    # Log response
    echo "Response: $response" >> $LOG_FILE
    
    # Check if successful
    if echo "$response" | grep -q "HTTP_STATUS:200"; then
        echo "Status: SUCCESS ✓" >> $LOG_FILE
    else
        echo "Status: FAILED ✗" >> $LOG_FILE
    fi
done

echo "" >> $LOG_FILE
echo "========================================" >> $LOG_FILE
echo "Daily Data Generation Completed" >> $LOG_FILE
echo "Date: $(date '+%Y-%m-%d %H:%M:%S')" >> $LOG_FILE
echo "========================================" >> $LOG_FILE
echo "" >> $LOG_FILE
