// hello.c - the smallest interesting program: a statically linked glibc binary.
//
// Even this touches hundreds of thousands of emulated instructions, because glibc's
// start-up runs before main(): TLS setup, ELF auxv parsing, stdio buffer allocation, locale init,
// getrandom() for stack-guard cookies, and the syscalls behind them all. It is the
// canonical smoke test that the VM's Linux syscall surface is complete enough to host a
// real libc.
//
// Build:  make examples          (from the repo root)
// Run:    ./scripts/run-elf.sh examples/build/hello

#include <stdio.h>

int main(void) {
    printf("Hello from inside the MLVM RISC-V virtual machine!\n");
    printf("Every instruction is visible in a deterministic RISC-V trace.\n");
    return 0;
}
