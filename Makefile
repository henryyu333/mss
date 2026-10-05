BINARY := mss
CMD := ./cmd/mss

.PHONY: build test vet lint install

build:
	go build -o $(BINARY) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run

install:
	go install $(CMD)
