BINARY      := accel-menu-go
VERSION     ?= 0.3.0
LDFLAGS     := -s -w -X main.version=$(VERSION)
GO          ?= go
HOST        ?= admin@bras.example.com
REMOTE_DIR  ?= /opt/docker/bin
REMOTE_TMP  ?= /tmp/accel-menu-go
CONFIG      ?= config.json
REMOTE_CFG  ?= /etc/accel-menu-go/config.json
SCP         ?= scp
SSH         ?= ssh

.PHONY: all build test race vet fmt fmt-check check deploy deploy-config remote-check clean

all: check build

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY) .

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w main.go internal

fmt-check:
	@out="$$(gofmt -l main.go internal)"; \
	if [ -n "$$out" ]; then echo "нужно выполнить gofmt:"; echo "$$out"; exit 1; fi

check: vet test fmt-check

deploy: build
	$(SCP) dist/$(BINARY) $(HOST):$(REMOTE_TMP)
	$(SSH) $(HOST) 'sudo -n install -m 0755 $(REMOTE_TMP) $(REMOTE_DIR)/$(BINARY)'

# Установка файла настроек с учётными данными (config.json не хранится в git).
deploy-config:
	$(SSH) $(HOST) 'sudo -n install -d -m 0750 /etc/accel-menu-go'
	$(SCP) $(CONFIG) $(HOST):/tmp/accel-menu-go.json
	$(SSH) $(HOST) 'sudo -n install -m 0600 /tmp/accel-menu-go.json $(REMOTE_CFG) && rm -f /tmp/accel-menu-go.json'

remote-check:
	$(SSH) $(HOST) 'sudo -n $(REMOTE_DIR)/$(BINARY) --check'

clean:
	rm -rf dist
