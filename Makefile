# All paths are repo-relative; run make from the repo root. No cd, no sed -i.
GO      ?= go
GOFMT   ?= gofmt
PYTHON  ?= python3
OPENAPI ?= api/openapi.yaml

.PHONY: check fmt vet test web-test spec-lint openapi-lint rule-coverage tasks-lint build

check: fmt vet test web-test spec-lint openapi-lint rule-coverage tasks-lint

build:
	$(GO) build ./...

fmt:
	@out="$$($(GOFMT) -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

NPM     ?= npm

# Vitest unit tests for web/ (no network once web/node_modules is installed).
web-test:
	@if [ -f web/package.json ]; then \
		[ -d web/node_modules ] || $(NPM) --prefix web install --no-audit --no-fund; \
		$(NPM) --prefix web test; \
	else echo "web-test: web/package.json not present, skipped"; fi

spec-lint:
	$(PYTHON) -I spec/eng/o2_lint.py --spec spec/legal --strict --baseline spec/eng/o2_lint.baseline.json

# The OpenAPI contract lands in T-002; until then there is nothing to lint.
openapi-lint:
	@if [ -f $(OPENAPI) ]; then \
		head -n 20 $(OPENAPI) | grep -Eq '^openapi: 3\.' || { echo "$(OPENAPI): missing 'openapi: 3.x' header"; exit 1; }; \
	else echo "openapi-lint: $(OPENAPI) not present yet, skipped"; fi

rule-coverage:
	$(PYTHON) -I tools/rule_coverage.py

tasks-lint:
	$(PYTHON) -I tools/check_tasks.py
