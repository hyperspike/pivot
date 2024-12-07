
ifeq ($V,1)
	Q =
	VV = -v
else
	Q = @
	VV =
endif

SRC = $(shell find . -name "*.go")

build: pivot

pivot: $(SRC)
	$Qgo build $(VV) -o $@ ./cmd/

.PHONY: clean
clean:
	$Qrm -f pivot
	$Qgo clean -cache -testcache -modcache

.PHONY: gosec lint
gosec:
	$Qgosec ./...

lint:
	$Qgolangci-lint run
