# yamlc -- a YAML 1.2 parser and a tool for finding where parsers disagree.
#
# Standard library only. GOPROXY is off so that a stray third-party import
# fails the build here rather than quietly fetching something.
GO      ?= go
GOFLAGS ?=
export GOPROXY = off
export GOTOOLCHAIN = local

all: yamlc

yamlc: $(wildcard *.go) go.mod
	$(GO) build $(GOFLAGS) -trimpath -o yamlc .

test: all
	./t/run.sh

vet:
	$(GO) vet ./...

clean:
	rm -f yamlc yamlc-mut
	rm -rf t/corpus t/tmp

.PHONY: all test vet clean
