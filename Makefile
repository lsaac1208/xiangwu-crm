.PHONY: install install-kxone-web check lint coverage security build

install: install-kxone-web

install-kxone-web:
	cd apps/kxone/web && npm ci

check:
	cd apps/kxone && go test ./...
	cd apps/kxone/web && npm test -- --run

lint:
	cd apps/kxone && go vet ./...
	cd apps/kxone/web && npm run lint

coverage:
	cd apps/kxone && go test -count=1 -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...
	cd apps/kxone && ../../scripts/check-go-coverage.sh coverage.out
	cd apps/kxone/web && npm run test:coverage
	cd apps/kxone/web && node ../../../scripts/check-js-coverage.mjs kxone-web coverage/coverage-summary.json

security:
	cd apps/kxone && go tool govulncheck ./...

build:
	cd apps/kxone && go build ./cmd/server ./cmd/worker
	cd apps/kxone/web && npm run build
