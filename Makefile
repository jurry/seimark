export CGO_ENABLED=0

.PHONY: test vet lint fmt-check build vectors

test:
	go test ./...

vet:
	go vet ./...
	GOARCH=386 go vet ./...

lint:
	golangci-lint run ./...

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

build:
	go build ./...

vectors:
	cd vectors/gen && ./gen.sh
