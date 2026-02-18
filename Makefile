.PHONY: help docs-dev docs-build docs-generate docs-serve sprout-build

help:
	@echo "Sprout Development Commands"
	@echo ""
	@echo "Documentation:"
	@echo "  make docs-dev        - Generate docs and start dev server"
	@echo "  make docs-build      - Build production documentation site"
	@echo "  make docs-generate   - Generate auto-generated documentation"
	@echo "  make docs-serve      - Serve production build locally"
	@echo ""
	@echo "Sprout:"
	@echo "  make sprout-build    - Build sprout binary"
	@echo "  make sprout-install  - Build and install sprout to /usr/local/bin"

docs-dev:
	cd apps/web && bun run dev

docs-build:
	cd apps/web && bun run build

docs-generate:
	cd apps/web && bun run docs:generate

docs-serve:
	cd apps/web && bun serve

sprout-build:
	cd apps/sprout && go build -o ../../sprout ./cmd/sprout

sprout-install: sprout-build
	sudo mv sprout /usr/local/bin/sprout
	@echo "Sprout installed to /usr/local/bin/sprout"
