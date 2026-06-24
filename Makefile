.PHONY: generate generate-agent-proto generate-proto build-agents test vet check

PROTOC ?= protoc
GOPATH_BIN := $(shell go env GOPATH)/bin
PROTOC_GEN_GO ?= $(GOPATH_BIN)/protoc-gen-go
PROTOC_GEN_GO_GRPC ?= $(GOPATH_BIN)/protoc-gen-go-grpc

PROTO_FILES := proto/agent_common.proto proto/hysteria.proto proto/amnezia.proto generated/proto/hysteria/agent.proto generated/proto/amnezia/agent.proto
AGENT_CMDS := ./cmd/lazgate-hysteria-agent ./cmd/lazgate-amnezia-agent

generate: generate-proto
	go generate ./...

generate-agent-proto:
	go run ./internal/protocol/cmd/agent-proto-gen -spec internal/legacy/agent/hysteria2/syncproto/hysteria.json -out generated/proto/hysteria/agent.proto -go-out generated/nodeproto/hysteria/manifest_builder.go
	go run ./internal/protocol/cmd/agent-proto-gen -spec internal/legacy/agent/amneziawg/syncproto/amnezia.json -out generated/proto/amnezia/agent.proto -go-out generated/nodeproto/amnezia/manifest_builder.go

generate-proto: generate-agent-proto
	$(PROTOC) \
		--plugin=protoc-gen-go=$(PROTOC_GEN_GO) \
		--plugin=protoc-gen-go-grpc=$(PROTOC_GEN_GO_GRPC) \
		--go_out=. \
		--go_opt=module=laz \
		--go-grpc_out=. \
		--go-grpc_opt=module=laz \
		$(PROTO_FILES)

build-agents:
	go build $(AGENT_CMDS)

test:
	go test ./...
	node --test tests/web/*.test.cjs

vet:
	go vet ./...

check: generate test vet
