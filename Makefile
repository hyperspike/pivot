
VERSION ?= $(shell  if [ ! -z $$(git tag --points-at HEAD) ] ; then git tag --points-at HEAD|cat ; else  git rev-parse --short HEAD|cat; fi )
SHA ?= $(shell git rev-parse --short HEAD)

ifeq ($V,1)
	Q =
	VV = -v
else
	Q = @
	VV =
endif

SRC = $(shell find . -name "*.go")
IMAGE ?= pivot

build: pivot

pivot: $(SRC)
	$QGCO_ENABLED=0 go build $(VV) \
		-trimpath \
		-installsuffix cgo \
		-ldflags "-s -w -X main.Version=$(VERSION) -X main.Commit=$(SHA)" \
		-o $@ ./cmd/

pivot-%: $(SRC)
	$QGOOS=$(shell echo $@ | cut -d- -f2) GOARCH=$(shell echo $@ | cut -d- -f3) \
	GCO_ENABLED=0 go build $(VV) \
		-trimpath \
		-installsuffix cgo \
		-ldflags "-s -w -X main.Version=$(VERSION) -X main.Commit=$(SHA)" \
		-o $@ ./cmd/

cli: pivot-darwin-amd64 pivot-linux-amd64 pivot-darwin-arm64 pivot-linux-arm64

.PHONY: gosec lint
gosec:
	$Qgosec ./...

lint:
	$Qgolangci-lint run

container: Dockerfile pivot
	$Qdocker build -t $(IMAGE) .

.PHONY: test
test:
	$Qgo test $(VV) ./...

.PHONY: clean real-clean
clean:
	$Qrm -rf pivot pivot-* infra postgres-operator

real-clean: clean
	$Qgo clean -cache -testcache -modcache
