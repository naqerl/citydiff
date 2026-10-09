vet: fmt
	go vet ./...

fmt:
	go fmt ./...

FILE ?= /home/user/Work/barse/service/flashcard/flashcard.go
RANGE ?= 6ca8b06f2209b8b57c209e4d9a448bdc0373ac87..3aff57d4730b6a615105d18cbe7eb611ae1ba4f7

run: vet
	go run ./cmd/cli -path "$(FILE)" $(ARGS)

diff: vet
	go run ./cmd/cli -path "$(FILE)" -range "$(RANGE)" $(ARGS)

VIEW_PATH ?= /home/user/Work/barse

view: vet
	go run ./cmd/cli -path "$(VIEW_PATH)" -view $(ARGS)
