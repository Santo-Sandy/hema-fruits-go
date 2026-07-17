#!/bin/bash

#############################################
# Test Script for Daily Data Generation
# Run this to test if everything is working
#############################################

echo "========================================="
echo "CERP Daily Data Generation - Test Script"
echo "========================================="
echo ""

# Configuration
API_URL="http://localhost:7002/activation-api/trigger-daily-data"
TEST_ORG_ID="org-2026-81"  # Testing with first org ID

echo "Step 1: Testing API connectivity..."
echo "URL: $API_URL"
echo ""

# Test if API is accessible
response=$(curl -s -w "\nHTTP_STATUS:%{http_code}" -X POST "${API_URL}?orgId=${TEST_ORG_ID}" --max-time 10)

echo "Response:"
echo "$response"
echo ""

# Check status
if echo "$response" | grep -q "HTTP_STATUS:200"; then
    echo "✅ SUCCESS: API is working!"
    echo ""
    echo "Step 2: Check your database for new records:"
    echo "  - Collection: purchase (created_by: 'SYSTEM')"
    echo "  - Collection: productions (created_by: 'SYSTEM')"
    echo "  - Collection: stock_ledger (created_by: 'SYSTEM')"
    echo ""
    echo "Step 3: Setup cron job:"
    echo "  Run: crontab -e"
    echo "  Add: 1 0 * * * $(pwd)/generate-daily-data.sh"
    echo ""
    echo "✅ Everything is ready!"
else
    echo "❌ FAILED: API is not responding correctly"
    echo ""
    echo "Troubleshooting:"
    echo "1. Is your application running?"
    echo "   Check: ps aux | grep cerp-api"
    echo ""
    echo "2. Is the API accessible?"
    echo "   Test: curl http://localhost:8080/health"
    echo ""
    echo "3. Did you update TEST_ORG_ID in this script?"
    echo "   Current value: $TEST_ORG_ID"
    echo ""
    echo "4. Is the organization configured for trial data?"
    echo "   Check: common_config.onboarding_configs collection"
fi

echo ""
echo "========================================="
