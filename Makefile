.PHONY: test exec bench bin/utt proto

PROJECT_ROOT:=$(shell pwd)
export GOPATH:=$(PROJECT_ROOT)/build
export PATH:=$(PROJECT_ROOT)/bin:$(PATH)

all: bin/utt

test:
	go test -v git.uestc.cn/sunmxt/utt/pkg/mux/...
	go test -v git.uestc.cn/sunmxt/utt/pkg/rpc/...

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

proto: bin/protoc-gen-go
	protoc -I=$(PROJECT_ROOT)/pkg --go_out=$(PROJECT_ROOT)/pkg proto/pb/core.proto 

exec:
	$(CMD)