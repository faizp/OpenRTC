.PHONY: lint typecheck test test-integration check

lint:
	./scripts/lint.sh

typecheck:
	./scripts/typecheck.sh

test:
	./scripts/test.sh

test-integration:
	./scripts/test-integration.sh

check:
	./scripts/lint.sh
	./scripts/typecheck.sh
	./scripts/test.sh
	./scripts/test-integration.sh
