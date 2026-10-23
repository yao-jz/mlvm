# Cross-compile the example programs to RV64IMAFD ELFs that MLVM can execute.
#
#   make            build every example into build/
#   make native     also build host binaries into build/native/ for determinism diffing
#   make clean
#
# The toolchain prefix defaults to riscv64-unknown-linux-gnu-, which is what
# riscv-gnu-toolchain installs. Override it if yours differs:
#
#   make CROSS=riscv64-linux-gnu-
#
# See ../docs/INSTALL.md for how to get a toolchain on macOS and Linux.

CROSS   ?= riscv64-unknown-linux-gnu-
CC      := $(CROSS)gcc
OBJDUMP := $(CROSS)objdump

# -march=rv64imafd: base integer + multiply + atomics + single + double float.
#
# Critically there is no 'c' here. The compressed extension emits 2-byte instructions,
# which this VM does not decode - an RVC binary reverts at step 0 with
# "unknown instruction opcode: 1".
#
# -static because there is no dynamic loader inside the VM, and -fno-stack-protector to
# avoid extra getrandom() traffic in the tiny examples.
#
# -ffp-contract=off makes the RISC-V and native comparison builds preserve the same source-
# level multiply/add sequence. MLVM implements fused FMADD semantics, but two compilers may
# otherwise make different contraction choices before either binary reaches its machine.
ARCH_FLAGS := -march=rv64imafd -mabi=lp64d
FP_FLAGS   := -ffp-contract=off
CFLAGS     := -O2 -static $(ARCH_FLAGS) $(FP_FLAGS) -fno-stack-protector
LDLIBS     := -lm

SRCS    := $(wildcard *.c)
TARGETS := $(patsubst %.c,build/%,$(SRCS))
NATIVE  := $(patsubst %.c,build/native/%,$(SRCS))

.PHONY: all native dump clean check-toolchain

all: check-toolchain $(TARGETS)

check-toolchain:
	@command -v $(CC) >/dev/null 2>&1 || { \
	  echo "error: $(CC) not found."; \
	  echo "       Install a RISC-V cross toolchain, or point CROSS= at yours."; \
	  echo "       See docs/INSTALL.md#risc-v-cross-toolchain"; \
	  exit 1; }

build:
	@mkdir -p build

build/native:
	@mkdir -p build/native

build/%: %.c mlvm_result.h | build
	$(CC) $(CFLAGS) -o $@ $< $(LDLIBS)
	@echo "  built $@"

# Host builds of the same sources, for ./scripts/determinism.sh to diff against.
native: $(NATIVE)

build/native/%: %.c mlvm_result.h | build/native
	$(or $(HOSTCC),cc) -O2 $(FP_FLAGS) -DMLVM_HOST_BUILD -o $@ $< -lm
	@echo "  built $@ (host)"

# Disassembly is genuinely useful when a program hits an unimplemented instruction:
# the VM prints the PC, and these files tell you what was at it.
dump: $(TARGETS)
	@for t in $(TARGETS); do \
	  $(OBJDUMP) -d --disassembler-options=no-aliases -M numeric $$t > $$t.dump; \
	  echo "  dumped $$t.dump"; \
	done

clean:
	rm -rf build
