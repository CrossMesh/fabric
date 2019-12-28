.PHONY: test exec bench bin/utt

PROJECT_ROOT:=$(shell pwd)
export GOPATH:=$(PROJECT_ROOT)/build
export PATH:=$(PROJECT_ROOT)/bin:$(PATH)

all: bin/utt

test:
	go test -bench=. -v git.uestc.cn/sunmxt/utt/pkg/...

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

exec:
	$(CMD)