all: build/traceme build/tracebrowser docker-image/traceme docker-image/tracebrowser
XDG_RUNTIME_DIR ?= /run/user/$(shell id -u)

# ===== Configuration =====
# Path to the app's source code
APP_PATH ?= $(shell realpath ../traceme-example-webcounter)

# Path to the trace directory (where compressed traces are stored from `traceme`)
TRACE_DIR ?= $(XDG_RUNTIME_DIR)/traceme-trace

# Path to the state directory (where extracted traces and code checkouts are stored)
STATE_DIR ?= $(XDG_RUNTIME_DIR)/traceme-state

# Name of the project (used to name the trace directory and the state directory)
PROJECT_NAME ?= webcounter

# Package name of the project (used to name the code checkout directory)
PROJECT_PACKAGE ?= github.com/wazzaps/traceme-example-webcounter
# ===== /Configuration =====

build/traceme: $(wildcard traceme/*.go)
	cd traceme && go build -o ../build/traceme main.go

build/tracebrowser: $(wildcard tracebrowser/*.go)
	cd tracebrowser && go build -o ../build/tracebrowser main.go

.PHONY: docker-image/traceme
docker-image/traceme: build/traceme
	docker build -f traceme.Dockerfile -t wazzaps/traceme .

.PHONY: docker-image/tracebrowser
docker-image/tracebrowser: build/tracebrowser docker-image/traceme
	docker build -f tracebrowser.Dockerfile -t wazzaps/tracebrowser .

.PHONY: start-tracebrowser
start-tracebrowser: docker-image/tracebrowser
	docker run --privileged --name traceme-browser --rm -it -d \
		-e TRACE_DIR=/var/traceme \
		-v "$(TRACE_DIR)":/var/traceme \
		-e STATE_DIR=/var/traceme-state \
		-v "$(STATE_DIR)":/var/traceme-state \
		-e CODE_SRC_DIR=/code/$(PROJECT_NAME) \
		-v "$(APP_PATH)":/code/$(PROJECT_NAME) \
		-e PROJECT_NAME=$(PROJECT_NAME) \
		-e PROJECT_PACKAGE="$(PROJECT_PACKAGE)" \
		-p 8090:8080 \
		wazzaps/tracebrowser

.PHONY: stop-tracebrowser
stop-tracebrowser:
	docker stop traceme-browser
