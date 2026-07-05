.PHONY: lint typecheck test test-integration coverage check production-check

lint:
	./scripts/lint.sh

typecheck:
	./scripts/typecheck.sh

test:
	./scripts/test.sh

test-integration:
	./scripts/test-integration.sh

coverage:
	./scripts/coverage.sh

check:
	./scripts/lint.sh
	./scripts/typecheck.sh
	./scripts/test.sh
	./scripts/test-integration.sh

production-check:
	./scripts/lint.sh
	./scripts/typecheck.sh
	./scripts/coverage.sh
	./scripts/test-integration.sh
