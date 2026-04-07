.PHONY: build test test-integration lint clean

build:
	go build ./...
	go build -o bin/lqworker ./cmd/lqworker

test:
	go test -v -race -count=1 -short ./...

test-integration:
	go test -v -race -count=1 -tags=integration ./...

lint:
	golangci-lint run

coverage:
	go test -v -race -count=1 -short -coverprofile=coverage.txt ./...
	go tool cover -html=coverage.txt -o coverage.html

clean:
	rm -rf bin/ coverage.txt coverage.html

tidy:
	go mod tidy
	go mod verify
