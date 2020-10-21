.PHONY: test exec bench bin/utt proto cover devtools mock env cloc tarball rpm srpm docker release
GOMOD:=github.com/crossmesh/fabric
PROJECT_ROOT:=$(shell pwd)
COVERAGE_DIR:=coverage
BUILD_DIR:=build

export GOPATH:=$(PROJECT_ROOT)/$(BUILD_DIR)
export PATH:=$(PROJECT_ROOT)/bin:$(GOPATH)/bin:$(PATH)

# build info.

VERSION_STRING:=$(shell git describe --tags `git rev-list --tags --max-count=1` || cat VERSION)
VERSION:=$(patsubst v%,%,$(VERSION_STRING))
BUILD_DATE:=$(shell date +%Y-%m-%dT%H:%M:%S)
REVISION:=$(shell git rev-parse HEAD || cat REVISION)

# construct build options
GCFLAGS:= all=
LDFLAGS:= all=
ASMFLAGS:= all=-trimpath '$(PROJECT_ROOT)'
BUILD_OPTS:= #{{BUILD_EXTRA_OPTS}}#

# for debug.
ifeq ($(TYPE),debug)
GCFLAGS+=-N -l
else
LDFLAGS+=-s -w
endif

# strip build paths.
GCFLAGS+= -trimpath $(PROJECT_ROOT)
# add version info to binary.
LDFLAGS+= -X $(GOMOD)/cmd/version.Revision=$(REVISION) \
			-X $(GOMOD)/cmd/version.Version=$(VERSION_STRING) \
			-X $(GOMOD)/cmd/version.BareVersion=$(VERSION) \
			-X $(GOMOD)/cmd/version.BuildDate=$(BUILD_DATE)

# final build opts.
ifneq ($(strip $(ASMFLAGS)),)
ASMFLAGS_OPT:=-asmflags="$(ASMFLAGS)"
endif
ifneq ($(strip $(LDFLAGS)),)
LDFLAGS_OPTS:=-ldflags="$(LDFLAGS)"
endif
ifneq ($(strip $(GCFLAGS)),)
GCFLAGS_OPTS:=-gcflags="$(GCFLAGS)"
endif


all: bin/utt

$(COVERAGE_DIR):
	mkdir -p $(COVERAGE_DIR)

cover: coverage test
	go tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html

test: coverage
	go test -v -bench=. -benchtime=2x -coverprofile=$(COVERAGE_DIR)/coverage.out -cover \
		./common \
		./mux/... \
		./gossip/... \
		./proto \
		./route \
		./cmd/version
	go tool cover -func=$(COVERAGE_DIR)/coverage.out

build:
	mkdir build

env:
	@echo "export PROJECT_ROOT=\"$(PROJECT_ROOT)\""
	@echo "export GOPATH=\"\$${PROJECT_ROOT}/build\""
	@echo "export PATH=\"\$${PROJECT_ROOT}/bin:\$${PATH}\""


cloc:
	cloc . --exclude-dir=build,bin,ci,mocks --exclude-ext=pb.go

mock: $(GOPATH)/bin/mockery

devtools: $(GOPATH)/bin/protoc-gen-go $(GOPATH)/bin/gopls $(GOPATH)/bin/goimports $(GOPATH)/bin/mockery

rpm:
	rpm/makerpm.sh rpm

srpm:
	rpm/makerpm.sh srpm

proto: $(GOPATH)/bin/protoc-gen-go $(GOPATH)/bin/protoc-gen-go-grpc
	protoc -I=$(PROJECT_ROOT) --go_out=module=$(GOMOD):. proto/pb/core.proto
	protoc -I=$(PROJECT_ROOT) --go_out=. --go-grpc_out=. --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative cmd/pb/core.proto
	find -E . -name '*.pb.go' -type f -not -path './build/*' | xargs sed -i '' "s|\"proto/pb\"|\"$(GOMOD)/proto/pb\"|g;"

docker: bin/utt
	docker build -t utt:$(VERSION) -f ./Dockerfile .
	docker tag utt:$(VERSION) utt:latest

exec:
	$(CMD)

tarball:
ifeq ($(BARE_PROJECT),yes)
	touch ./utt-$(VERSION).tar.gz
	ln -s "`pwd`" ./utt-$(VERSION)
	tar -hzvcf ./utt-$(VERSION).tar.gz utt-$(VERSION) \
	        --exclude='utt-$(VERSION)/utt-$(VERSION).tar.gz' \
	        --exclude='utt-$(VERSION)/utt-$(VERSION)'
	rm ./utt-$(VERSION)
else
	mkdir -p utt-$(VERSION)
	echo $(REVISION) > utt-$(VERSION)/REVISION
	echo $(VERSION) > utt-$(VERSION)/VERSION
	go list ./... | grep -E '$(GOMOD)' | sed -E 's|$(GOMOD)||g; /^$$/d; s|/(.*)|\1|; s|/.*||g' | sort | uniq \
		| sed -E 's|(.*)|\1|' | xargs -n 1 -I {} cp -r {} utt-$(VERSION)/{}
	cp go.mod go.sum main.go README.md utt.yml Dockerfile utt-$(VERSION)/
	cp -rv rpm systemd utt-$(VERSION)/
	echo 'BARE_PROJECT:=yes' > utt-$(VERSION)/Makefile
	sed -E 's|#\{\{BUILD_EXTRA_OPTS\}\}#|-mod vendor|g' Makefile >> utt-$(VERSION)/Makefile
	cd utt-$(VERSION); go mod vendor
	tar -zvcf ./utt-$(VERSION).tar.gz utt-$(VERSION)
	rm -rf utt-$(VERSION)
endif

build/bin: bin build
	test -d build/bin || ln -s $$(pwd)/bin build/bin

bin/utt: build/bin
	go build -o bin/utt -v $(GCFLAGS_OPTS) $(LDFLAGS_OPTS) $(ASMFLAGS_OPT) $(BUILD_OPTS) $(GOMOD)

release: $(PROJECT_ROOT)/releases
	GOOS=linux GOARCH=arm go build -o $(PROJECT_ROOT)/releases/utt-$(VERSION)-linux-arm \
		-v $(GCFLAGS_OPTS) $(LDFLAGS_OPTS) $(ASMFLAGS_OPT) $(BUILD_OPTS) $(GOMOD)
	GOOS=linux GOARCH=arm64 go build -o $(PROJECT_ROOT)/releases/utt-$(VERSION)-linux-arm64 \
		-v $(GCFLAGS_OPTS) $(LDFLAGS_OPTS) $(ASMFLAGS_OPT) $(BUILD_OPTS) $(GOMOD)
	GOOS=linux GOARCH=386 go build -o $(PROJECT_ROOT)/releases/utt-$(VERSION)-linux-386 \
		-v $(GCFLAGS_OPTS) $(LDFLAGS_OPTS) $(ASMFLAGS_OPT) $(BUILD_OPTS) $(GOMOD)
	GOOS=linux GOARCH=amd64 go build -o $(PROJECT_ROOT)/releases/utt-$(VERSION)-linux-amd64 \
		-v $(GCFLAGS_OPTS) $(LDFLAGS_OPTS) $(ASMFLAGS_OPT) $(BUILD_OPTS) $(GOMOD)
	GOOS=darwin GOARCH=amd64 go build -o $(PROJECT_ROOT)/releases/utt-$(VERSION)-darwin-amd64 \
		-v $(GCFLAGS_OPTS) $(LDFLAGS_OPTS) $(ASMFLAGS_OPT) $(BUILD_OPTS) $(GOMOD)
	GOOS=darwin GOARCH=386 go build -o $(PROJECT_ROOT)/releases/utt-$(VERSION)-darwin-386 \
		-v $(GCFLAGS_OPTS) $(LDFLAGS_OPTS) $(ASMFLAGS_OPT) $(BUILD_OPTS) $(GOMOD)

$(PROJECT_ROOT)/releases bin:
	mkdir "$@"

$(GOPATH)/bin/protoc-gen-go: build/bin
	go get -u github.com/golang/protobuf/protoc-gen-go

$(GOPATH)/bin/gopls: build/bin
	go get -u golang.org/x/tools/gopls

$(GOPATH)/bin/goimports: build/bin
	go get -u golang.org/x/tools/cmd/goimports

$(GOPATH)/bin/mockery: build/bin
	go get -u github.com/vektra/mockery/cmd/mockery

$(GOPATH)/bin/protoc-gen-go-grpc: build/bin
	go get -u google.golang.org/grpc/cmd/protoc-gen-go-grpc
