# Aurelia — build & operations targets.
# See docs/OPERATIONS.md for the full guide.

BINARY        := $(HOME)/.aurelia/bin/aurelia
TMP_BINARY    := $(BINARY).new
PKG           := ./cmd/aurelia
SERVICE_LABEL := com.aurelia.agent
SERVICE       := gui/$(shell id -u)/$(SERVICE_LABEL)
LOG_DIR       := $(HOME)/.aurelia/logs
STDERR_LOG    := $(LOG_DIR)/aurelia.stderr.log
STDOUT_LOG    := $(LOG_DIR)/aurelia.stdout.log

.PHONY: help build test vet bridge install install-service deploy restart stop status logs stdout uninstall-service

help:
	@echo "Common targets:"
	@echo "  make build            Compile the binary to $(BINARY)"
	@echo "  make test             Run go test ./... -short"
	@echo "  make vet              Run go vet ./..."
	@echo "  make bridge           Rebuild the TS bridge bundle"
	@echo ""
	@echo "Service targets (macOS launchd):"
	@echo "  make install-service  Install/refresh launchd plist (run once after fresh clone)"
	@echo "  make deploy           Build atomically + kick the service"
	@echo "  make restart          Restart the service without rebuilding"
	@echo "  make stop             Stop the service (bootout)"
	@echo "  make status           Show launchd state for the service"
	@echo "  make logs             Tail stderr log"
	@echo "  make stdout           Tail stdout log"
	@echo "  make uninstall-service Remove plist and unload"

# --- Build ---

build:
	mkdir -p $(dir $(BINARY))
	go build -o $(BINARY) $(PKG)

# Atomic build: write to .new then rename so a running daemon never sees a
# half-written file. On macOS the running process keeps its mmap of the old
# inode, so this is safe even with the service active.
install:
	mkdir -p $(dir $(BINARY))
	go build -o $(TMP_BINARY) $(PKG)
	mv $(TMP_BINARY) $(BINARY)

test:
	go test ./... -short -count=1

vet:
	go vet ./...

bridge:
	cd bridge && npx esbuild index.ts --bundle --platform=node --target=node18 --outfile=bundle.js --format=esm
	cp bridge/bundle.js internal/bridge/bundle.js

# --- Service (launchd) ---

install-service:
	./scripts/install-service.sh

# Atomic deploy: build + swap + kickstart. Use this for every change.
deploy: install
	@if launchctl print $(SERVICE) >/dev/null 2>&1; then \
		launchctl kickstart -k $(SERVICE); \
		echo "deployed: $(BINARY) (service kicked)"; \
	else \
		echo "warning: service not loaded — run 'make install-service' first"; \
		echo "binary updated at: $(BINARY)"; \
	fi

restart:
	@if launchctl print $(SERVICE) >/dev/null 2>&1; then \
		launchctl kickstart -k $(SERVICE); \
		echo "service restarted"; \
	else \
		echo "service not loaded — run 'make install-service' first" >&2; \
		exit 1; \
	fi

stop:
	@if launchctl print $(SERVICE) >/dev/null 2>&1; then \
		launchctl bootout $(SERVICE); \
		echo "service stopped"; \
	else \
		echo "service not loaded"; \
	fi

status:
	@if launchctl print $(SERVICE) >/dev/null 2>&1; then \
		launchctl print $(SERVICE) | awk '/^\tstate = |^\tpid = |^\tlast exit code = |^\tprogram = |^\tpath = /'; \
	else \
		echo "service not loaded"; \
		exit 1; \
	fi

logs:
	@test -f $(STDERR_LOG) || { echo "log not found: $(STDERR_LOG)" >&2; exit 1; }
	tail -n 50 -f $(STDERR_LOG)

stdout:
	@test -f $(STDOUT_LOG) || { echo "log not found: $(STDOUT_LOG)" >&2; exit 1; }
	tail -n 50 -f $(STDOUT_LOG)

uninstall-service:
	@if launchctl print $(SERVICE) >/dev/null 2>&1; then \
		launchctl bootout $(SERVICE) || true; \
	fi
	rm -f $(HOME)/Library/LaunchAgents/$(SERVICE_LABEL).plist
	@echo "service unloaded and plist removed"
