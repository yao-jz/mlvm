// matmul.c - dense 32x32 double-precision matrix multiply.
//
// This is the determinism test that matters for on-chain ML. The VM never uses the host
// FPU: every FADD.D / FMUL.D / FSQRT.D is executed by the Go port of Berkeley SoftFloat in
// rvgo/comp, in the rounding mode the instruction selects (round-to-nearest-even here, since
// nothing changes frm) and with a fixed NaN policy. Compile this file for the
// host as well and the two runs must print byte-identical digits, on any machine.
//
// Build:  make examples
// Run:    ./scripts/run-elf.sh examples/build/matmul
// Check:  ./scripts/determinism.sh matmul

#include <stdio.h>
#include <math.h>

#define N 32

static double A[N][N], B[N][N], C[N][N];

int main(void) {
    for (int i = 0; i < N; i++) {
        for (int j = 0; j < N; j++) {
            A[i][j] = (double)((i * 31 + j * 17) % 13) * 0.25;
            B[i][j] = (double)((i * 7 + j * 3) % 11) * 0.5;
        }
    }

    for (int i = 0; i < N; i++) {
        for (int j = 0; j < N; j++) {
            double s = 0.0;
            for (int k = 0; k < N; k++) {
                s += A[i][k] * B[k][j];
            }
            C[i][j] = s;
        }
    }

    double trace = 0.0, frob = 0.0;
    for (int i = 0; i < N; i++) {
        trace += C[i][i];
        for (int j = 0; j < N; j++) {
            frob += C[i][j] * C[i][j];
        }
    }

    printf("trace     = %.17g\n", trace);
    printf("frobenius = %.17g\n", sqrt(frob));
    printf("C[0][0]   = %.17g\n", C[0][0]);
    printf("C[31][31] = %.17g\n", C[N - 1][N - 1]);
    return 0;
}
