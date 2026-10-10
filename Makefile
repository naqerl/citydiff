vet: fmt
	go vet ./...

test: vet
	go test ./...
	node --test view/*.test.mjs

fmt:
	go fmt ./...

# File or directory to parse. Defaults to the checkout make is run in.
# A file path still uses the git repo that contains it for RANGE.
FILE ?= .

# Git toplevel when FILE is inside a checkout, otherwise FILE's directory.
repo_dir = $(shell f='$(FILE)'; if [ -d "$$f" ]; then start=$$f; else start=$$(dirname "$$f"); fi; top=$$(git -C "$$start" rev-parse --show-toplevel 2>/dev/null) || true; if [ -n "$$top" ]; then printf '%s' "$$top"; else cd "$$start" && pwd; fi)

# older..newer of the two latest commits in a checkout.
two_latest = $(shell git -C '$(1)' rev-list --max-count=2 HEAD 2>/dev/null | awk 'NR==1 { new = $$0 } NR==2 { print $$0 ".." new }')

ifeq ($(origin RANGE), undefined)
RANGE = $(call two_latest,$(repo_dir))
endif

run: vet
	go run ./cmd/cli -path "$(FILE)" $(ARGS)

diff: vet
	@test -n "$(RANGE)" || { printf 'diff: %s needs two commits, or pass RANGE=A..B\n' "$(repo_dir)" >&2; exit 1; }
	@printf 'citydiff: %s %s\n' "$(FILE)" "$(RANGE)" >&2
	go run ./cmd/cli -path "$(FILE)" -range "$(RANGE)" $(ARGS)

view: vet
	@printf 'citydiff: %s %s\n' "$(FILE)" "$(if $(RANGE),$(RANGE),working tree)" >&2
	go run ./cmd/cli -path "$(FILE)" $(if $(RANGE),-range "$(RANGE)") -view $(ARGS)

IMAGE ?= citydiff:$(shell git rev-parse --short HEAD 2>/dev/null || echo latest)
PORT ?= 8787
DOCKER_PATH ?= $(repo_dir)

ifeq ($(origin DOCKER_RANGE), undefined)
DOCKER_RANGE = $(call two_latest,$(DOCKER_PATH))
endif

# FILE as seen in the container, where DOCKER_PATH is mounted at /work.
# A FILE outside that mount uses the mount itself.
container_path = $(shell root='$(abspath $(DOCKER_PATH))'; f='$(abspath $(FILE))'; if [ "$$f" = "$$root" ]; then printf '/work'; else rel=$$(realpath --relative-to="$$root" "$$f" 2>/dev/null || true); prefix=$$(printf '%s' "$$rel" | cut -c1-3); if [ -z "$$rel" ] || [ "$$rel" = .. ] || [ "$$prefix" = ../ ]; then printf '/work'; else printf '/work/%s' "$$rel"; fi; fi)

run-docker:
	@test -d "$(DOCKER_PATH)" || { printf 'run-docker: %s is not a directory\n' "$(DOCKER_PATH)" >&2; exit 1; }
	docker build -t "$(IMAGE)" .
	@printf 'citydiff %s: http://127.0.0.1:%s (%s)\n' "$(IMAGE)" "$(PORT)" "$(if $(DOCKER_RANGE),$(DOCKER_RANGE),working tree)"
	docker run --rm --name "citydiff-$(PORT)" \
		-p "$(PORT):8787" \
		-v "$(abspath $(DOCKER_PATH)):/work" \
		"$(IMAGE)" \
		-path "$(container_path)" -view -addr 0.0.0.0:8787 $(if $(DOCKER_RANGE),-range "$(DOCKER_RANGE)") $(ARGS)
