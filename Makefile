# All paths are repo-relative; run make from the repo root. No cd, no sed -i.
GO      ?= go
GOFMT   ?= gofmt
PYTHON  ?= python3
OPENAPI ?= api/openapi.yaml

.PHONY: check fmt vet test web-test spec-lint openapi-lint rule-coverage tasks-lint ai-fixture-lint migration-lint build

check: fmt vet test web-test spec-lint openapi-lint rule-coverage tasks-lint ai-fixture-lint migration-lint

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

# Vitest unit tests, ESLint (float ban) and tsc for web/ (no network once web/node_modules is installed).
web-test:
	@if [ -d web/node_modules ]; then $(NPM) --prefix web test && $(NPM) --prefix web run lint && $(NPM) --prefix web run typecheck; \
	else echo "web-test: web/node_modules not present, skipped (run npm --prefix web install)"; fi

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

# Q-AI-003: every feature with a fixture dir or a ticked fixture task needs its fixtures.
ai-fixture-lint:
	$(PYTHON) -I tools/ai_fixture_lint.py --self-test
	$(PYTHON) -I tools/ai_fixture_lint.py


# MIG-060 / MIG-061 on the shipped migrations. Only the frozen 0001_init.sql has a baseline.
migration-lint:
	$(PYTHON) -I tools/migration_lint.py --baseline tools/migration_lint_baseline.txt migrations
