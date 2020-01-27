.PHONY: test exec bench bin/utt proto cover devtools

PROJECT_ROOT:=$(shell pwd)
export GOPATH:=$(PROJECT_ROOT)/build
export PATH:=$(PROJECT_ROOT)/bin:$(PATH)

COVERAGE_DIR:=coverage

all: bin/utt

$(COVERAGE_DIR):
	mkdir -p $(COVERAGE_DIR)

cover: coverage test
	go tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html

test: coverage
	go test -v -coverprofile=$(COVERAGE_DIR)/coverage.out ./mux/... ./rpc/... ./gossip/... ./proto
	go tool cover -func=$(COVERAGE_DIR)/coverage.out

build:
	mkdir build

bin:
	mkdir bin

build/bin: bin build
	test -d build/bin || ln -s $$(pwd)/bin build/bin

bin/utt:
	@if [ "$${TYPE:=release}" = "debug" ]; then 					\
		go install -v -gcflags='all=-N -l' git.uestc.cn/sunmxt/utt; \
	else																\
	    go install -v -ldflags='all=-s -w' git.uestc.cn/sunmxt/utt; \
	fi

bin/protoc-gen-go:
	go get -u github.com/golang/protobuf/protoc-gen-go

bin/gopls:
	go get -u go get -u golang.org/x/tools/gopls

bin/goimports:
	go get -u go get -u golang.org/x/tools/cmd/goimports

devtools: bin/protoc-gen-go bin/gopls bin/goimports

proto: bin/protoc-gen-go
	protoc -I=$(PROJECT_ROOT) --go_out=$(PROJECT_ROOT) proto/pb/core.proto 

exec:
	$(CMD)