vet: fmt
	go vet ./...

test: vet
	go test ./...
	node --test view/

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

IMAGE ?= citydiff:$(shell git rev-parse --short HEAD 2>/dev/null || echo latest)
PORT ?= 8787
DOCKER_PATH ?= $(VIEW_PATH)
DOCKER_RANGE ?=

run-docker:
	@test -d "$(DOCKER_PATH)" || { printf 'run-docker: %s is not a directory\n' "$(DOCKER_PATH)" >&2; exit 1; }
	docker build -t "$(IMAGE)" .
	@printf 'citydiff %s: http://127.0.0.1:%s\n' "$(IMAGE)" "$(PORT)"
	docker run --rm --name "citydiff-$(PORT)" \
		-p "$(PORT):8787" \
		-v "$(abspath $(DOCKER_PATH)):/work" \
		"$(IMAGE)" \
		-path /work -view -addr 0.0.0.0:8787 $(if $(DOCKER_RANGE),-range "$(DOCKER_RANGE)") $(ARGS)
