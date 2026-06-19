.PHONY: test

test:
	go test -race -cover -count=100 ./...
