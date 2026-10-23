/* mlvm_result.h - commit a guest's answer to the machine's own state.
 *
 * An MLVM state witness commits its memory root, but stdout is not part of that commitment.
 * A result that exists only in output logs therefore cannot be authenticated against a
 * proposed post-state.
 *
 * So the guest writes its answer into memory at a fixed absolute address. The final memory
 * root then commits to it, and MLClaim.sol can read it back with one merkle branch. A future
 * outer protocol could identify
 *
 *     (prestate, poststate, answer)
 *
 * while independently establishing prestate -> poststate. MLClaim.sol only performs the
 * post-state readback; it does not validate that execution or implement a dispute game.
 *
 * The window is 64 bytes because MLVM's merkle leaves are 32 bytes and MLVM_RESULT_BASE is
 * 64-byte aligned, so the two leaves are siblings and one 60-node branch carries both.
 *
 *     +0   u64  magic    "MLVM_OUT", so an untouched page cannot read as a valid answer
 *     +8   u64  length   payload bytes, at most MLVM_RESULT_MAX
 *     +16  ...  payload
 *
 * Usage:
 *
 *     #include "mlvm_result.h"
 *     ...
 *     struct { double probs[4]; uint64_t argmax; } answer = { ... };
 *     mlvm_commit_result(&answer, sizeof answer);
 */

#ifndef MLVM_RESULT_H
#define MLVM_RESULT_H

#include <stdint.h>

/* Clear of every other MLVM region: the ELF image sits near 0, the heap at 0x7f<<40, the
 * stack at 2^60, randomness at 0x1500..., staged files at 0x2000..., and the float
 * sub-game's operands at 0x3000... */
#define MLVM_RESULT_BASE 0x3100000000000000ULL
#define MLVM_RESULT_MAGIC 0x4D4C564D5F4F5554ULL /* "MLVM_OUT" */
#define MLVM_RESULT_MAX 48

static inline void mlvm_commit_result(const void *data, uint64_t len) {
#ifdef MLVM_HOST_BUILD
    /* Native comparison binaries have no MLVM address space. Their stdout is the
     * determinism oracle, so committing to the guest-only result page is unnecessary. */
    (void)data;
    (void)len;
#else
    volatile uint64_t *hdr = (volatile uint64_t *)MLVM_RESULT_BASE;
    volatile unsigned char *payload = (volatile unsigned char *)(MLVM_RESULT_BASE + 16);
    const unsigned char *src = (const unsigned char *)data;
    uint64_t i;

    if (len > MLVM_RESULT_MAX) {
        len = MLVM_RESULT_MAX; /* truncate rather than scribble past the window */
    }
    for (i = 0; i < MLVM_RESULT_MAX; i++) {
        payload[i] = (i < len) ? src[i] : 0; /* zero the tail: the leaf is committed whole */
    }
    hdr[1] = len;
    hdr[0] = MLVM_RESULT_MAGIC; /* written last, so a partial write is never valid */
#endif
}

#endif /* MLVM_RESULT_H */
