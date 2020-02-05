.PHONY: test exec bench bin/utt proto cover devtools mock env cloc dist

GOMOD:=git.uestc.cn/sunmxt/utt

PROJECT_ROOT:=$(shell pwd)
BUILD_DIR:=build
VERSION:=$(shell cat VERSION)
REVISION:=$(shell git rev-parse HEAD)
export GOPATH:=$(PROJECT_ROOT)/$(BUILD_DIR)
export PATH:=$(PROJECT_ROOT)/bin:$(GOPATH)/bin:$(PATH)

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

devtools: $(GOPATH)/bin/protoc-gen-go $(GOPATH)/bin/gopls $(GOPATH)/bin/goimports $(GOPATH)/bin/mockery

proto: bin/protoc-gen-go
	protoc -I=$(PROJECT_ROOT) --go_out=$(PROJECT_ROOT) proto/pb/core.proto
	protoc -I=$(PROJECT_ROOT) --go_out=plugins=grpc:$(PROJECT_ROOT) control/rpc/pb/core.proto
	find -E . -name '*.pb.go' -type f -not -path './build/*' | xargs sed -i '' "s|\"proto/pb\"|\"$(GOMOD)/proto/pb\"|g; s|\"control/rpc/pb\"|\"$(GOMOD)/control/rpc/pb\"|g"

exec:
	$(CMD)

dist:
	rm -rf $(GOPATH)/pkg/mod
	mkdir -p utt-$(VERSION)
	echo $(REVISION) > utt-$(VERSION)/REVISION
	go list ./... | grep -E '$(GOMOD)' | sed -E 's|$(GOMOD)||g; /^$$/d; s|/(.*)|\1|; s|/.*||g' | sort | uniq \
		| sed -E 's|(.*)|\1|' | xargs -n 1 -I {} cp -r {} utt-$(VERSION)/{}
	mkdir -p utt-$(VERSION)/$(BUILD_DIR)/pkg/mod
	find $(BUILD_DIR)/pkg/mod -not -path '$(BUILD_DIR)/pkg/mod/cache' -type d -depth 1 | xargs -n 1 -I {} cp -r {} utt-$(VERSION)/{}
	cp go.mod go.sum main.go Makefile README.md VERSION utt-$(VERSION)/
	tar -zvcf ./utt-$(VERSION).tar.gz utt-$(VERSION)
	rm -rf utt-$(VERSION)

build/bin: bin build
	test -d build/bin || ln -s $$(pwd)/bin build/bin

bin/utt: build/bin
	@if [ "$${TYPE:=release}" = "debug" ]; then 					\
		go build -v -gcflags='all=-N -l' -o bin/utt $(GOMOD); \
	else																\
	    go build -v -ldflags='all=-s -w' -o bin/utt $(GOMOD); \
	fi

$(GOPATH)/bin/protoc-gen-go:
	go get -u github.com/golang/protobuf/protoc-gen-go

$(GOPATH)/bin/gopls:
	go get -u golang.org/x/tools/gopls

$(GOPATH)/bin/goimports:
	go get -u golang.org/x/tools/cmd/goimports

$(GOPATH)/bin/mockery:
	go get -u github.com/vektra/mockery/cmd/mockery
