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

# One single edit to the parser at a time, rebuilt, with the whole suite
# run against each. About half a minute per mutant. A group name does one
# group: python3 t/mutants.py resolve
mutants: all
	python3 t/mutants.py

vet:
	$(GO) vet ./...

clean:
	rm -f yamlc yamlc-mut
	rm -rf t/corpus t/tmp

.PHONY: all test mutants vet clean
