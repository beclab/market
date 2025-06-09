#!/bin/bash

# Simple script to run the Go program with environment variables
# 运行 Go 程序并加载环境变量的简单脚本

echo "Setting up environment variables..."

# Load key environment variables
# 加载关键环境变量
export SYNCER_REMOTE_HASH_URL="https://appstore-server-test.bttcdn.com/api/v1/appstore/hash"
export SYNCER_REMOTE_DATA_URL="http://localhost:8080/api/data"
export SYNCER_DETAIL_URL_TEMPLATE="http://localhost:8080/api/detail/%s"
export SYNCER_SYNC_INTERVAL="5m"

export REDIS_HOST="localhost"
export REDIS_PORT="6379"
export REDIS_PASSWORD=""
export REDIS_DB="0"
export REDIS_TIMEOUT="5s"

export POSTGRES_HOST="localhost"
export POSTGRES_PORT="5432"
export POSTGRES_DB="mydb"
export POSTGRES_USER="postgres"
export POSTGRES_PASSWORD="mysecret"

export MODULE_ENABLE_SYNC="true"
export MODULE_ENABLE_CACHE="true"
export MODULE_START_TIMEOUT="30s"

echo "Environment variables set. Starting application..."
echo "SYNCER_REMOTE_HASH_URL: $SYNCER_REMOTE_HASH_URL"

# Run the Go program
# 运行 Go 程序
go run cmd/market/v2/main.go 