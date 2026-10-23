# Build the TinyStories Llama2-C decoder guest for MLVM.
CROSS   ?= riscv64-unknown-linux-gnu-
CC      := $(CROSS)gcc
HOSTCC  ?= cc

ARCH_FLAGS := -march=rv64imafd -mabi=lp64d
# Fuse a*b+c into one fmadd.s, as GCC does by default for GNU C: matmul's inner product
# becomes one fused multiply-add per weight, which MLVM executes with a single rounding.
# Greedy tokens match a host build with or without contraction on every prompt we have
# compared, but the bits of intermediate values follow the guest's instruction choice.
FP_FLAGS   := -ffp-contract=fast
CFLAGS     := -O2 -static $(ARCH_FLAGS) $(FP_FLAGS) -fno-stack-protector -Wall -Wextra
LDLIBS     := -lm

.PHONY: all native request clean check-toolchain

all: check-toolchain build/decoder

check-toolchain:
	@command -v $(CC) >/dev/null 2>&1 || { \
	  echo "error: $(CC) not found. See docs/INSTALL.md"; exit 1; }

build:
	@mkdir -p build

build/decoder: decoder.c ../mlvm_result.h | build
	$(CC) $(CFLAGS) -o $@ decoder.c $(LDLIBS)
	@echo "  built $@"

native: build/decoder-host

build/decoder-host: decoder.c ../mlvm_result.h | build
	$(HOSTCC) -O2 $(FP_FLAGS) -DMLVM_HOST_BUILD -Wall -Wextra -o $@ decoder.c -lm
	@echo "  built $@ (host)"

request: build/request.bin

build/request.bin: mkrequest.py | build
	python3 mkrequest.py "Once upon a time" -o $@

clean:
	rm -rf build
