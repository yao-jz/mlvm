# MLVM - deterministic RISC-V execution and proof tooling
#
# Run `make` with no arguments for the list of targets.
# New here? `make doctor` tells you what is installed and what is missing, then
# `make demo` runs a language model inside the VM and prints its execution receipt.

RVSOL     := rvsol
GO        ?= go
FORGE     ?= forge
BIN_DIR   := bin
MLVM      := $(BIN_DIR)/mlvm
MLVM_CMD  := ./cmd/mlvm
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS   := -X main.version=$(VERSION) -X main.commit=$(COMMIT)

# Colours, disabled when not writing to a terminal or when NO_COLOR is set.
ifeq ($(shell test -t 1 && test -z "$$NO_COLOR" && echo yes),yes)
  B := \033[1m
  D := \033[2m
  R := \033[0m
else
  B :=
  D :=
  R :=
endif

.DEFAULT_GOAL := help
.PHONY: help doctor build examples check fmt-check docs-check scripts-check vet test test-go test-vm test-fd test-sol test-all bench \
        determinism proof isa models model-determinism float-placement charts \
        float-dispute float-subgame float-nesting ml-claim hello mlp fmt clean deep-clean demo tour \
        decoder decoder-pack demo-verify archive-smoke evm-artifact web

help:
	@printf "$(B)MLVM$(R) - deterministic RV64IMAFD execution and proof tooling\n\n"
	@printf "$(B)getting started$(R)\n"
	@printf "  make doctor         check which toolchains are installed\n"
	@printf "  make build          build the mlvm CLI into ./bin\n"
	@printf "  make examples       cross-compile the C examples to RV64IMAFD\n"
	@printf "  make demo           TinyStories decoder demo (receipt + --verify)\n"
	@printf "  make web            build the browser playground into web/dist\n"
	@printf "  bin/mlvm liar       catch a machine that lies about one instruction\n"
	@printf "  bin/mlvm xray       exact instruction-level profile and flame graph\n"
	@printf "  make archive-smoke  fail-closed canonical receipt digest\n"
	@printf "  make tour           longer execution + witness walkthrough\n\n"
	@printf "$(B)maintaining$(R)\n"
	@printf "  make check          format, docs, shell, vet and default Go tests\n"
	@printf "  make test-all       all Go and Solidity cross-checks\n\n"
	@printf "$(B)running programs$(R)\n"
	@printf "  make hello          run a glibc hello-world inside the VM\n"
	@printf "  make mlp            run a neural-network forward pass inside the VM\n"
	@printf "  ./scripts/run-elf.sh <elf> [files...]   run any RV64IMAFD ELF\n\n"
	@printf "$(B)verifying$(R)\n"
	@printf "  make test           RISC-V conformance suites (integer, mul, atomics, float)\n"
	@printf "  make test-fd        just the RV32/64 F and D floating-point suites\n"
	@printf "  make test-sol       build and test the Solidity mirror of the VM\n"
	@printf "  make evm-artifact   refresh the RISCV.sol bytecode embedded in mlvm liar\n"
	@printf "  make test-all       everything, including the slow and EVM cross-checks\n"
	@printf "  make determinism    diff VM float results against your native FPU\n"
	@printf "  make model-determinism   same, on real transformer models\n"
	@printf "  make proof          walk the single-step witness pipeline\n"
	@printf "  make float-dispute  exercise the experimental f64 terminal path in an EVM\n"
	@printf "  make float-subgame  check the nested float machine against rvgo/comp\n"
	@printf "  make ml-claim       bind answer bytes to a proposed post-state\n\n"
	@printf "$(B)measuring$(R)\n"
	@printf "  make bench          throughput, witness size and instruction mix\n"
	@printf "  make models         run real BERT checkpoints through the VM\n"
	@printf "  make charts         regenerate the figures from the results\n"
	@printf "  make float-placement  interpreter vs compiler float emulation, in steps\n"
	@printf "  make float-nesting    marginal RV64I cost of one f64 operation\n"
	@printf "  make isa            instruction coverage report\n\n"
	@printf "$(B)housekeeping$(R)\n"
	@printf "  make fmt            format the Go sources\n"
	@printf "  make clean          remove build output\n"
	@printf "  make deep-clean     also remove Rust target/ dirs and forge artifacts\n\n"
	@printf "$(D)docs: README.md, then docs/INSTALL.md and docs/ARCHITECTURE.md$(R)\n"

# ---------------------------------------------------------------- environment

doctor:
	@printf "$(B)required$(R)\n"
	@printf "  %-28s %s\n" "go (>= 1.23)"      "$$(command -v $(GO) >/dev/null 2>&1 && $(GO) version | awk '{print $$3}' || echo 'MISSING - https://go.dev/dl/')"
	@printf "  %-28s %s\n" "python3"           "$$(command -v python3 >/dev/null 2>&1 && python3 --version | awk '{print $$2}' || echo 'MISSING')"
	@printf "\n$(B)to build guest programs$(R)\n"
	@cc=''; for c in riscv64-unknown-linux-gnu-gcc riscv64-linux-gnu-gcc riscv64-unknown-elf-gcc; do \
	   if command -v $$c >/dev/null 2>&1; then cc="$$c $$($$c -dumpversion)"; break; fi; done; \
	 printf "  %-28s %s\n" "riscv64 gcc" "$${cc:-MISSING - see docs/INSTALL.md}"
	@printf "\n$(B)to work on the on-chain verifier$(R)\n"
	@printf "  %-28s %s\n" "forge (foundry)"   "$$(command -v $(FORGE) >/dev/null 2>&1 && $(FORGE) --version | head -1 | awk '{print $$2}' || echo 'MISSING - https://getfoundry.sh')"
	@printf "\n$(B)to rebuild the Rust ML guests$(R)\n"
	@printf "  %-28s %s\n" "rustc (nightly)"   "$$(command -v rustc >/dev/null 2>&1 && rustc --version | awk '{print $$2}' || echo 'MISSING - https://rustup.rs')"
	@printf "  %-28s %s\n" "riscv rust target" "$$(rustup target list --installed 2>/dev/null | grep -q riscv64 && echo installed || echo 'MISSING - rustup target add riscv64gc-unknown-linux-gnu')"
	@printf "\n$(D)Only Go and python3 are needed to build and run the VM itself.$(R)\n"

# ---------------------------------------------------------------- building

build:
	@mkdir -p $(BIN_DIR)
	@printf "$(B)==>$(R) building MLVM $(VERSION)\n"
	@# Build and sign a private copy, then rename it into place, so a run already using
	@# $(MLVM) never sees a half-written or re-signed file.
	@$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(MLVM).tmp $(MLVM_CMD)
	@# macOS: the Go linker emits an ad-hoc "linker-signed" signature that some security
	@# configurations refuse, killing the binary with SIGKILL and no message. Re-signing
	@# ad-hoc fixes it and is harmless when it was already fine.
	@if [ "$$(uname -s)" = "Darwin" ] && command -v codesign >/dev/null 2>&1; then \
	   codesign -s - -f $(MLVM).tmp >/dev/null 2>&1 || true; fi
	@mv -f $(MLVM).tmp $(MLVM)
	@printf "    $(MLVM)\n"

examples:
	@printf "$(B)==>$(R) cross-compiling examples to RV64IMAFD\n"
	@$(MAKE) -s -C examples all

# ---------------------------------------------------------------- running

hello: examples
	@./scripts/run-elf.sh examples/build/hello

mlp: examples
	@./scripts/run-elf.sh examples/build/mlp

# ---------------------------------------------------------------- testing

# The default suite: instruction-level conformance in fast mode. This is the one that has
# to stay green. See docs/ARCHITECTURE.md#three-vms-one-semantics for why the slow and EVM
# cross-checks are opt-in.
test:
	@printf "$(B)==>$(R) RISC-V conformance suites (fast mode)\n"
	@$(GO) test ./rvgo/test/ -run 'TestFastStep' -count=1

test-go:
	@printf "$(B)==>$(R) all Go tests (optional slow/EVM paths disabled)\n"
	@$(GO) test ./... -count=1

test-vm: test

test-fd:
	@printf "$(B)==>$(R) floating-point conformance (RV32/64 F and D)\n"
	@$(GO) test ./rvgo/test/ -run 'TestFastStepFD' -count=1 -v 2>&1 \
	  | grep -E '^(=== RUN|    --- |        --- |ok|FAIL)|known failure|now passes' \
	  | sed 's/^/  /'

test-sol:
	@printf "$(B)==>$(R) building and testing the Solidity VM\n"
	@cd $(RVSOL) && $(FORGE) build && $(FORGE) test

# The browser playground: mlvm.wasm, the page, and a local determinism.json. Serve with
#   python3 -m http.server -d web/dist 8000
web:
	@bash scripts/build-web.sh web/dist

# Refresh the RISCV.sol bytecode that `mlvm liar` runs in an in-process EVM. Run after any
# change to rvsol/src/RISCV.sol; internal/evmstep's test fails while it is stale.
evm-artifact:
	@cd $(RVSOL) && $(FORGE) build
	@python3 -c "import json; d=json.load(open('$(RVSOL)/out/RISCV.sol/RISCV.json')); open('rvgo/internal/evmstep/RISCV.deployed.hex','w').write(d['deployedBytecode']['object'][2:]+'\n'); open('rvgo/internal/evmstep/RISCV.solc','w').write(json.loads(d['rawMetadata'])['compiler']['version']+'\n')"
	@printf "  refreshed rvgo/internal/evmstep/RISCV.deployed.hex\n"

# Includes the slow-mode and EVM cross-checks. `test-sol` builds the artifacts consumed by
# the embedded-EVM tests before the Go suite starts.
test-all: test-sol
	@printf "$(B)==>$(R) all Go tests, including slow/EVM cross-checks\n"
	@MLVM_RUN_SLOW=1 MLVM_RUN_EVM=1 $(GO) test ./... -count=1

# ---------------------------------------------------------------- measuring

bench: examples
	@./scripts/bench.sh

# Runs real transformer checkpoints through the VM. Needs the Rust guest built and will
# download several hundred MB of weights on first use.
models:
	@./scripts/model-bench.sh --fetch

# Same transformer, same weights, run natively and inside the VM, scores compared.
model-determinism:
	@./scripts/model-determinism.sh

# One kernel, built for rv64imafd and rv64ima, run in the same VM: is floating point
# cheaper to emulate in the interpreter or in the compiler?
float-placement:
	@./scripts/float-placement.sh

# The commitment layer: bind answer bytes to a proposed post-state so a future protocol can
# identify the application result of a state transition.
ml-claim:
	@./scripts/ml-claim.sh

# Route C prototype: a real program and a real EVM exercise the terminal path for a
# disputed f64 instruction without any IEEE-754 in Solidity. This is not a complete game.
float-dispute:
	@./scripts/float-dispute.sh

# The differential property underneath it: the nested machine computes exactly what the
# VM's own SoftFloat computes across all 22 operations and the 1,522 vectors used by the
# Yul tests. Also checks rvgo/comp against the host FPU.
float-subgame: build
	@printf "$(B)==>$(R) the nested float machine vs rvgo/comp, and both vs the EVM\n"
	@$(GO) test ./rvgo/test/ -run 'TestFloatSubGame|TestSoftFloat' -count=1 -v \
	    | grep -E 'vectors|NaN|bit-identical|exact|mean|gas|result|PASS|FAIL|SKIP'

# The marginal cost of one f64 operation, by differencing builds.
float-nesting:
	@./scripts/float-nesting.sh

charts:
	@./scripts/gen-charts.py

determinism: examples
	@./scripts/determinism.sh

proof: examples
	@./scripts/fraud-proof-demo.sh

isa:
	@./scripts/isa-coverage.py

# ---------------------------------------------------------------- demos

# One-command launch demo: the embedded TinyStories decoder, with the MLVM_OUT commit store
# cross-checked by slow.Step. No cross compiler or download required.
demo: build
	@$(MLVM) demo --verify

demo-verify: demo

# TinyStories decoder pack + guest (needs RISC-V GCC and a fetched model pack).
decoder:
	@$(MAKE) -s -C examples/decoder

decoder-pack: decoder
	@./scripts/decoder-fetch.sh
	@./scripts/decoder-bench.sh

tour: build examples
	@printf "\n$(B)################  1/4  a C program running inside the VM  ################$(R)\n"
	@./scripts/run-elf.sh examples/build/hello
	@printf "\n$(B)################  2/4  a neural network running inside the VM  ##########$(R)\n"
	@./scripts/run-elf.sh examples/build/mlp
	@printf "\n$(B)################  3/4  softfloat vs your hardware FPU  ##################$(R)\n"
	@./scripts/determinism.sh
	@printf "\n$(B)################  4/4  the single-step witness pipeline  ################$(R)\n"
	@./scripts/fraud-proof-demo.sh
	@printf "\n$(B)Done.$(R) Next: docs/ARCHITECTURE.md for how it works, docs/ISA.md for what it supports.\n"

# ---------------------------------------------------------------- housekeeping

check: fmt-check docs-check scripts-check vet test-go

fmt-check:
	@files="$$(gofmt -l $$(find cmd rvgo -name '*.go' -type f))"; \
	 test -z "$$files" || { printf "these files need gofmt:\n%s\n" "$$files"; exit 1; }
	@printf "gofmt: clean\n"

docs-check:
	@python3 scripts/check-links.py

scripts-check:
	@for script in scripts/*.sh; do bash -n "$$script" || exit 1; done
	@printf "shell syntax: clean\n"

vet:
	@$(GO) vet ./...
	@printf "go vet: clean\n"

fmt:
	@$(GO) fmt ./... >/dev/null && printf "gofmt: clean\n"

clean:
	@rm -rf $(BIN_DIR) .mlvm-run .mlvm-proof
	@$(MAKE) -s -C examples clean
	@printf "cleaned build output\n"

deep-clean: clean
	@rm -rf $(RVSOL)/out $(RVSOL)/cache
	@rm -rf examples/rust/*/target third_party/*/target
	@printf "cleaned Rust target/ dirs and forge artifacts\n"
