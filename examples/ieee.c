// ieee.c - a deterministic IEEE-754 regression workload.
//
// Every value below is irrational-ish: no operand is exactly representable, so each of the
// ~16,000 arithmetic operations has to round, and any disagreement about rounding shows up
// in the printed digits. The program deliberately walks the hard parts of IEEE 754:
//
//   * a long dependent chain of / and + so rounding errors accumulate rather than cancel
//   * sqrt, which has its own correctly-rounded requirement
//   * repeated doubling of the smallest subnormal (5e-324), which exercises gradual
//     underflow and the tininess-after-rounding rule
//   * a shrinking product plus 1e-300, which lands results in the subnormal range
//
// Build it for the host and for RV64IMAFD and the two must print identical digits. MLVM
// achieves that by never using the host FPU: rvgo/comp is a Go port of Berkeley SoftFloat
// 3 with a fixed NaN policy, called from fast/vm.go for every F/D instruction in the
// rounding mode that instruction selects. This file never changes frm, so every operation
// here rounds to nearest, ties to even - the host default too.
//
// Note what this file does NOT use: no libm transcendentals (tanh, exp, pow). Those are
// library code, not ISA semantics, and glibc's versions differ from Apple's or musl's in
// the last ULP. See examples/mlp.c for what that looks like in practice, and
// docs/BENCHMARKS.md#floating-point-determinism for the full analysis.
//
// Build:  make examples
// Check:  ./scripts/determinism.sh ieee

#include <stdio.h>
#include <math.h>

int main(void) {
    // A dependent chain: every iteration's rounding feeds the next.
    double a = 1.0, s = 0.0, p = 1.0;
    for (int i = 1; i <= 2000; i++) {
        a = a / 3.0 + 1.0 / (double)i;
        s += a * a;
        p = p * 1.0000001 - a / 7.0;
    }
    printf("sum       = %.17g\n", s);
    printf("product   = %.17g\n", p);
    printf("sqrt(sum) = %.17g\n", sqrt(s));

    // Gradual underflow: start at the smallest positive subnormal and double it.
    double tiny = 5e-324, acc = 0.0;
    for (int i = 0; i < 50; i++) {
        acc = acc * 2.0 + tiny;
    }
    printf("subnormal = %.17g\n", acc);

    // Drive a normal value down into the subnormal range.
    double t = 0.5;
    for (int i = 0; i < 20; i++) {
        t = t * 3.0 / 7.0 + 1e-300;
    }
    printf("underflow = %.17g\n", t);

    return 0;
}
