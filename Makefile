.PHONY: build test run run-publisher
export

ARGS ?=

build:
	go build -o actographer ./cmd/actographer/

test:
	go test -v -count=1 ./...

run:
	go run ./cmd/actographer/ $(ARGS)

run-publisher:
	go run ./cmd/actographer/ --mode publisher $(ARGS)
