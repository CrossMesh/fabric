.PHONY: test exec bench

PROJECT_ROOT:=$(shell pwd)
export GOPATH:=$(PROJECT_ROOT)/build
export PATH:=$(PROJECT_ROOT)/bin:$(PATH)

test:
	go test -bench=. -v git.uestc.cn/sunmxt/utt

build:
	mkdir build

bin:
	mkdir bin

build/bin: bin
	test -d build/bin || ln -s $$(pwd)/bin build/bin

exec:
	$(CMD)