#!/usr/bin/make -f

BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
COMMIT := $(shell git log -1 --format='%H')

# don't override user values
ifeq (,$(VERSION))
  VERSION := $(shell git describe --tags)
  # if VERSION is empty, then populate it with branch's name and raw commit hash
  ifeq (,$(VERSION))
    VERSION := $(BRANCH)-$(COMMIT)
  endif
endif

# default value, overide with: make -e FQCN="foo"
FQCN = ghcr.io/defiantlabs/redpointswap

all: install

install: go.sum
	go install .

build:
	go build -o bin/redpointswap .

clean:
	rm -rf build

build-docker-amd:
	docker build -t $(FQCN) -f ./Dockerfile \
	--build-arg TARGETPLATFORM=linux/amd64 .

build-docker-arm:
	docker build -t $(FQCN) -f ./Dockerfile \
	--build-arg TARGETPLATFORM=linux/arm64 .
