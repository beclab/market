# Copyright 2022 bytetrade
# 
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
# 
#     http://www.apache.org/licenses/LICENSE-2.0
# 
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Build the manager binary
FROM golang:1.22.2-alpine as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod bytetrade.io/web3os/market/go.mod
COPY go.sum bytetrade.io/web3os/market/go.sum

# Copy the go source
COPY cmd/ bytetrade.io/web3os/market/cmd/
COPY pkg/ bytetrade.io/web3os/market/pkg/
COPY internal/ bytetrade.io/web3os/market/internal/


# Build
RUN cd bytetrade.io/web3os/market && \
        go mod tidy 

RUN cd bytetrade.io/web3os/market && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -a -o market cmd/market/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM alpine:latest
WORKDIR /opt/app
COPY --from=builder /workspace/bytetrade.io/web3os/market/market .

CMD ["/opt/app/market", "-v", "4", "--logtostderr"]
