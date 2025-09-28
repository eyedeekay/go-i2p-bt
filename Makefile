all: fmt test

fmt:
	find . -name '*.go' -exec gofumpt -w -s -extra {} \;

test:
	go test -v -race ./...