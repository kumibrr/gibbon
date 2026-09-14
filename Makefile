.PHONY: build test install vet fmt

build:
	go build -o gibbon ./cmd/gibbon

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w ./cmd ./internal

install:
	go install ./cmd/gibbon
