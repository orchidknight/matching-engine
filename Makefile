install-lint:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v2.2.2

lint:
	@golangci-lint --version
	@golangci-lint run