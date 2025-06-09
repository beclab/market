#!/bin/bash

# Load environment variables from dev.env file
# 从 dev.env 文件加载环境变量

echo "Loading environment variables from dev.env..."

# Read and export variables, skipping comments and empty lines
# 读取并导出变量，跳过注释和空行
while IFS= read -r line || [[ -n "$line" ]]; do
    # Skip comments and empty lines
    # 跳过注释和空行
    if [[ $line =~ ^[[:space:]]*# ]] || [[ -z "${line// }" ]]; then
        continue
    fi
    
    # Export the variable
    # 导出变量
    if [[ $line =~ ^[[:space:]]*([^=]+)=(.*)$ ]]; then
        export "${BASH_REMATCH[1]}"="${BASH_REMATCH[2]}"
        echo "Exported: ${BASH_REMATCH[1]}=${BASH_REMATCH[2]}"
    fi
done < dev.env

echo "Environment variables loaded successfully!"
echo ""
echo "Key variables:"
echo "  SYNCER_REMOTE_HASH_URL: $SYNCER_REMOTE_HASH_URL"
echo "  REDIS_HOST: $REDIS_HOST"
echo "  POSTGRES_HOST: $POSTGRES_HOST" 