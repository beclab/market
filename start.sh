#!/bin/bash

# Startup script for Market API Server

echo "Starting Market API Server..."
echo "Loading environment variables from dev.env..."

# Enable automatic export of variables when sourcing
set -a
source dev.env
set +a

echo "Environment variables loaded:"
echo "  SYNCER_REMOTE_HASH_URL: $SYNCER_REMOTE_HASH_URL"
echo "  SYNCER_REMOTE_DATA_URL: $SYNCER_REMOTE_DATA_URL"
echo "  SYNCER_DETAIL_URL_TEMPLATE: $SYNCER_DETAIL_URL_TEMPLATE"
echo "  SYNCER_SYNC_INTERVAL: $SYNCER_SYNC_INTERVAL"
echo "  REDIS_HOST: $REDIS_HOST"
echo "  POSTGRES_HOST: $POSTGRES_HOST"
echo ""

# Set Go proxy to resolve network issues
echo "Setting Go proxy to resolve network issues..."
export GOPROXY=https://goproxy.cn,direct
export GOSUMDB=sum.golang.google.cn

# Run the main program
echo "Starting application..."
go run cmd/market/v2/main.go 