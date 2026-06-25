.PHONY: generate generate-proto test vet check

PROTOC ?= protoc
GOPATH_BIN := $(shell go env GOPATH)/bin
PROTOC_GEN_GO ?= $(GOPATH_BIN)/protoc-gen-go
PROTOC_GEN_GO_GRPC ?= $(GOPATH_BIN)/protoc-gen-go-grpc

PROTO_FILES := proto/agentcontrol.proto

generate: generate-proto
	go generate ./...

generate-proto:
	$(PROTOC) \
		--plugin=protoc-gen-go=$(PROTOC_GEN_GO) \
		--plugin=protoc-gen-go-grpc=$(PROTOC_GEN_GO_GRPC) \
		--go_out=. \
		--go_opt=module=laz \
		--go-grpc_out=. \
		--go-grpc_opt=module=laz \
		$(PROTO_FILES)

test:
	go test ./...

vet:
	go vet ./...

check: generate test vet
