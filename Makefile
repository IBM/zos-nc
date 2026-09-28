BINS:=nc

# Version string baked into the binary via -ldflags "-X main.version=...".
# Priority: $ZOSNC_VERSION (explicit release override) ->
# `git describe` (e.g. v1.0.5, or v1.0.5-1-g701c100 on commits past a tag) ->
# dev-<build timestamp> fallback for non-git builds (e.g. dev-20260207_153045).
VERSION ?= $(ZOSNC_VERSION)
ifeq ($(strip $(VERSION)),)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev-$(shell date +%Y%m%d_%H%M%S))
endif
LDFLAGS := -X main.version=$(VERSION)

all: $(BINS)

ME:=$(firstword $(MAKEFILE_LIST))

nc: nc.go connectproxy.go $(ME)
	go build -ldflags "$(LDFLAGS)" -o nc
	-goz-util -c nc


clean:
	-@ [ -x nc ] && rm nc

check:
	@echo no checks yet

install:
	mkdir -p $(PREFIX)/bin
	install $(BINS) $(PREFIX)/bin
