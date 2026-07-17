#!/bin/bash

echo "🚀 Testing Daily Data Generation for Your Organizations..."
echo ""
echo "Organization 1: org-2026-81"
echo "Organization 2: org-2026-80"
echo ""
echo "Testing..."
echo ""

# Test org-2026-81
echo "Testing org-2026-81..."
response1=$(curl -s -X POST "http://localhost:7002/activation-api/trigger-daily-data?orgId=org-2026-81")
echo "Response: $response1"
echo ""

# Test org-2026-80
echo "Testing org-2026-80..."
response2=$(curl -s -X POST "http://localhost:7002/activation-api/trigger-daily-data?orgId=org-2026-80")
echo "Response: $response2"
echo ""

# Check if successful
if echo "$response1" | grep -q "triggered"; then
    echo "✅ org-2026-81: SUCCESS"
else
    echo "❌ org-2026-81: FAILED"
fi

if echo "$response2" | grep -q "triggered"; then
    echo "✅ org-2026-80: SUCCESS"
else
    echo "❌ org-2026-80: FAILED"
fi

echo ""
echo "Check your database for new records:"
echo "  db.purchase.find({created_by: 'SYSTEM'})"
echo "  db.productions.find({created_by: 'SYSTEM'})"
echo "  db.stock_ledger.find({created_by: 'SYSTEM'})"
echo ""
echo "View logs:"
echo "  cat /var/log/cerp-daily-data.log"
echo "  OR"
echo "  cat ~/cerp-daily-data.log"
