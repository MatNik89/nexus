BIN := bin/nexus

.PHONY: build check test clean

build:
	CGO_ENABLED=0 go build -trimpath -o $(BIN) ./cmd/nexus

check: build
	go vet ./...
	./scripts/static-check.sh $(BIN)

test:
	CGO_ENABLED=0 go test ./...

clean:
	rm -rf bin
