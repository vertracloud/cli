.PHONY: build test deps dist

build:
	go build -o ./bin/vertra ./cmd/vertra

test:
	go vet ./... && go test ./...

# Resolve o SDK publicado (ignora o go.work local) e atualiza o go.sum.
deps:
	GOWORK=off go mod tidy

# make dist VERSION=v0.1.0 — gera os pacotes de todos os sistemas em dist/ (não publica nada).
dist: deps test
	bash scripts/package.sh $(VERSION)
