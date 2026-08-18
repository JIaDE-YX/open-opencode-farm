# OpenCode Farm - native Ubuntu management
# Use `make help` to see available commands.

SHELL := /bin/bash
FARM := ./bin/farm

.PHONY: help doctor install build gateway egress status

help:
	@echo "OpenCode Farm (native Ubuntu)"
	@echo ""
	@echo "  make doctor          - check dependencies and data layout"
	@echo "  make install         - install native egress binary"
	@echo "  make build           - build gateway binary"
	@echo "  make gateway         - run the gateway service"
	@echo "  make egress          - run the egress proxy service"
	@echo "  make status          - show service health"

doctor:
	$(FARM) doctor

install:
	$(FARM) install-egress

build:
	$(FARM) build

gateway:
	$(FARM) gateway

egress:
	$(FARM) egress

status:
	$(FARM) status
