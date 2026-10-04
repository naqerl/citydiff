vet: fmt
	go vet ./...

fmt:
	go fmt ./...

FILE ?= /home/user/Work/barse/service/flashcard/flashcard.go

run: vet
	go run ./cmd/cli -path "$(FILE)" $(ARGS)
