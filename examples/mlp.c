// mlp.c - forward pass of a small multi-layer perceptron, in pure C.
//
// This is MLVM's thesis in 100 lines: a neural network whose every activation is a
// verifiable RISC-V instruction. The network is a 16 -> 32 -> 32 -> 4 MLP with tanh
// hidden layers and a softmax head. Weights are generated deterministically from a fixed
// seed so the program has no inputs and no I/O beyond stdout - which makes its output a
// pure function of the ISA semantics, and therefore something two parties can dispute
// bit-by-bit.
//
// It exercises exactly the operations that dominate real inference: multiply/add pairs in
// the inner GEMM loop, exp() and tanh() from libm (which decompose into FMUL/FADD/FDIV plus integer
// exponent surgery), and FDIV.D for the softmax normalisation.
//
// Build:  make examples
// Run:    ./scripts/run-elf.sh examples/build/mlp
// Check:  ./scripts/determinism.sh mlp

#include <stdio.h>
#include <math.h>
#include "mlvm_result.h"

#define IN   16
#define H1   32
#define H2   32
#define OUT  4

static double w1[H1][IN], b1[H1];
static double w2[H2][H1], b2[H2];
static double w3[OUT][H2], b3[OUT];
static double x[IN], h1[H1], h2[H2], logits[OUT], probs[OUT];

// A tiny LCG: deterministic, portable, and free of any libc randomness so that the
// weights are identical on the host and inside the VM.
static unsigned long long rng_state = 0x2545F4914F6CDD1DULL;

static double next_weight(void) {
    rng_state = rng_state * 6364136223846793005ULL + 1442695040888963407ULL;
    // Take 20 bits, map to [-0.5, 0.5) with an exactly representable scale.
    return (double)((rng_state >> 33) & 0xFFFFF) / 1048576.0 - 0.5;
}

static void init(void) {
    for (int i = 0; i < H1; i++) { for (int j = 0; j < IN; j++) w1[i][j] = next_weight(); b1[i] = next_weight(); }
    for (int i = 0; i < H2; i++) { for (int j = 0; j < H1; j++) w2[i][j] = next_weight(); b2[i] = next_weight(); }
    for (int i = 0; i < OUT; i++) { for (int j = 0; j < H2; j++) w3[i][j] = next_weight(); b3[i] = next_weight(); }
    for (int j = 0; j < IN; j++) x[j] = next_weight();
}

// dense computes y = act(W x + b). `act` is 1 for tanh, 0 for identity.
static void dense(const double *W, const double *b, const double *in,
                  double *out, int rows, int cols, int act) {
    for (int i = 0; i < rows; i++) {
        double s = b[i];
        for (int j = 0; j < cols; j++) {
            s += W[i * cols + j] * in[j];   // multiply/add-heavy inner loop
        }
        out[i] = act ? tanh(s) : s;
    }
}

static void softmax(const double *in, double *out, int n) {
    double m = in[0];
    for (int i = 1; i < n; i++) if (in[i] > m) m = in[i];
    double sum = 0.0;
    for (int i = 0; i < n; i++) { out[i] = exp(in[i] - m); sum += out[i]; }
    for (int i = 0; i < n; i++) out[i] /= sum;
}

int main(void) {
    init();

    dense(&w1[0][0], b1, x,  h1,     H1,  IN, 1);
    dense(&w2[0][0], b2, h1, h2,     H2,  H1, 1);
    dense(&w3[0][0], b3, h2, logits, OUT, H2, 0);
    softmax(logits, probs, OUT);

    printf("MLP 16-32-32-4, tanh hidden, softmax head\n");
    for (int i = 0; i < OUT; i++) {
        printf("logit[%d] = % .17g   p[%d] = %.17g\n", i, logits[i], i, probs[i]);
    }

    int argmax = 0;
    for (int i = 1; i < OUT; i++) if (probs[i] > probs[argmax]) argmax = i;

    double checksum = 0.0;
    for (int i = 0; i < H2; i++) checksum += h2[i] * (double)(i + 1);

    printf("argmax    = %d\n", argmax);
    printf("checksum  = %.17g\n", checksum);

    /* Commit the answer to the machine's own state, so a contract can read it out of the
     * final memory root rather than having to trust stdout. See mlvm_result.h. */
    struct {
        double probs[OUT];
        uint64_t argmax;
    } answer;
    for (int i = 0; i < OUT; i++) answer.probs[i] = probs[i];
    answer.argmax = (uint64_t)argmax;
    mlvm_commit_result(&answer, sizeof answer);

    return 0;
}
