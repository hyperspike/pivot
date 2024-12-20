
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
		-o $@ ./cmd/

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
	$Qrm -f pivot infra postgres-operator

real-clean: clean
	$Qgo clean -cache -testcache -modcache
