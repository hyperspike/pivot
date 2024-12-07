
SRC = $(shell find . -name "*.go")

build: pivot

pivot: $(SRC)
	go build -v -o $@ ./cmd/
