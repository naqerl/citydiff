vet: fmt
	go vet ./...

fmt:
	go fmt ./...

run: vet
	go run main.go
