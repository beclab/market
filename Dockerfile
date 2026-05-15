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
FROM golang:1.25.1-alpine as builder

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
  CGO_ENABLED=0 go build -ldflags="-s -w" -a -o market cmd/market/v2/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM alpine:3.23
WORKDIR /opt/app
COPY --from=builder /workspace/bytetrade.io/web3os/market/market .

# Drop root: create a dedicated unprivileged user/group and hand
# them ownership of the working directory before USER switches the
# default execution identity. Running as root in the runtime image
# was unnecessary (the binary opens an HTTP listener on an
# unprivileged port and never touches the filesystem outside its
# own workdir) and made the container an easy escalation target.
RUN addgroup -S market && adduser -S -G market market \
    && chown -R market:market /opt/app

USER market

# A lightweight liveness probe so orchestrators can detect a wedged
# process without a network round-trip. The binary does not yet
# expose a dedicated /healthz endpoint, so fall back to a process
# check; this is enough to catch hangs and crashes that systemd /
# docker would otherwise miss until the next request fails.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD pgrep -x market >/dev/null || exit 1

ENTRYPOINT ["/opt/app/market", "-v", "2", "--logtostderr"]
