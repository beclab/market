#!/bin/bash

# Test script for Settings API
# 设置API测试脚本

BASE_URL="http://localhost:8080/api/v2"

echo "Testing Settings API..."
echo "======================"

# Test 1: Get market source configuration
echo "1. Testing GET /settings/market-source"
curl -X GET "${BASE_URL}/settings/market-source" \
  -H "Content-Type: application/json" \
  | jq '.'

echo -e "\n"

# Test 2: Set market source configuration
echo "2. Testing PUT /settings/market-source"
curl -X PUT "${BASE_URL}/settings/market-source" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://new-market-source.example.com"}' \
  | jq '.'

echo -e "\n"

# Test 3: Get market source configuration again to verify update
echo "3. Testing GET /settings/market-source (after update)"
curl -X GET "${BASE_URL}/settings/market-source" \
  -H "Content-Type: application/json" \
  | jq '.'

echo -e "\n"

# Test 4: Test with invalid data
echo "4. Testing PUT /settings/market-source with empty URL"
curl -X PUT "${BASE_URL}/settings/market-source" \
  -H "Content-Type: application/json" \
  -d '{"url": ""}' \
  | jq '.'

echo -e "\n"

echo "Testing completed!" 