.PHONY: test exec bench bin/utt proto cover devtools mock env cloc

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
	go test -v -coverprofile=$(COVERAGE_DIR)/coverage.out -cover ./mux/... ./rpc/... ./gossip/... ./proto ./route ./arbiter
	go tool cover -func=$(COVERAGE_DIR)/coverage.out

build:
	mkdir build

env:
	@echo "export PROJECT_ROOT=\"$(PROJECT_ROOT)\""
	@echo "export GOPATH=\"\$${PROJECT_ROOT}/build\""
	@echo "export PATH=\"\$${PROJECT_ROOT}/bin:\$${PATH}\""

bin:
	mkdir bin

cloc:
	cloc . --exclude-dir=build,bin,ci,mocks

mock: bin/mockery
	bin/mockery -name=Backend -dir=./backend -output=./backend/mocks -outpkg=mocks

devtools: bin/protoc-gen-go bin/gopls bin/goimports bin/mockery

proto: bin/protoc-gen-go
	protoc -I=$(PROJECT_ROOT) --go_out=$(PROJECT_ROOT) proto/pb/core.proto 

exec:
	$(CMD)

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
	go get -u golang.org/x/tools/gopls

bin/goimports:
	go get -u golang.org/x/tools/cmd/goimports

bin/mockery:
	go get -u github.com/vektra/mockery/cmd/mockery
