export GO111MODULE=on

# Canonical version of this in https://github.com/coreos/coreos-assembler/blob/6eb97016f4dab7d13aa00ae10846f26c1cd1cb02/Makefile#L19
GOARCH:=$(shell uname -m)
ifeq ($(GOARCH),x86_64)
	GOARCH=amd64
else ifeq ($(GOARCH),aarch64)
	GOARCH=arm64
else ifeq ($(patsubst armv%,arm,$(GOARCH)),arm)
	GOARCH=arm
else ifeq ($(patsubst i%86,386,$(GOARCH)),386)
	GOARCH=386
endif

.PHONY: all
all:
	./build

.PHONY: vendor
vendor:
	@go mod vendor
	@go mod tidy

