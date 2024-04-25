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

.PHONY: market fmt vet linux scp

all: market

tidy: 
	go mod tidy
	
fmt: ;$(info $(M)...Begin to run go fmt against code.) @
	go fmt ./...

vet: ;$(info $(M)...Begin to run go vet against code.) @
	go vet ./...

ci-lint:
	golangci-lint run ./...

lint:
	golint ./...

market: tidy fmt vet ;$(info $(M)...Begin to build market.) @
	go build -o output/market cmd/market/main.go

linux: tidy fmt vet
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o market cmd/market/main.go

run: fmt vet; $(info $(M)...Run market.)
	go run cmd/market/main.go -v 4 --logtostderr

.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

