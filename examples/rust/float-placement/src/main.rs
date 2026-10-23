//! Where should floating point be emulated: in the interpreter, or in the compiler?
//!
//! MLVM executes `FADD.D` directly, through the software IEEE-754 implementation in
//! `rvgo/comp`. The alternative — what most verifiable virtual machines do — is to
//! build the guest without the F and D extensions at all, so the *compiler* expands each
//! floating-point operation into a soft-float helper made of integer instructions, and the
//! VM then emulates those.
//!
//! This kernel is built both ways from one source and run in the same VM. The metric is
//! emulated step count, which is the right one: it is hardware-independent, it determines
//! replay cost during bisection, and for a zero-knowledge VM it determines proving cost
//! linearly.
//!
//! `no_std` on purpose. There is no libc, no allocator, no libm and no dynamic loader, so
//! the step-count difference between the two builds is attributable to floating point and
//! nothing else. It also means the soft-float build needs no sysroot: Rust's
//! `compiler_builtins` supplies `__adddf3`, `__muldf3`, `__divdf3` and the rest.
//!
//! Run it with ./scripts/float-placement.sh

#![no_std]
#![no_main]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_: &PanicInfo) -> ! {
    exit(1)
}

/// Raw Linux syscall. MLVM implements the same ABI: a7 selects the call, a0-a2 carry
/// arguments, and the result comes back in a0.
#[inline(always)]
unsafe fn syscall3(n: usize, a0: usize, a1: usize, a2: usize) -> usize {
    let ret: usize;
    core::arch::asm!(
        "ecall",
        in("a7") n,
        inlateout("a0") a0 => ret,
        in("a1") a1,
        in("a2") a2,
        options(nostack)
    );
    ret
}

fn write(bytes: &[u8]) {
    unsafe { syscall3(64, 1, bytes.as_ptr() as usize, bytes.len()) };
}

fn exit(code: usize) -> ! {
    unsafe { syscall3(93, code, 0, 0) };
    loop {}
}

/// Print a f64's exact bit pattern. Comparing digits would hide a one-ULP difference,
/// and the whole point is to show the two builds agree exactly.
fn write_bits(label: &[u8], v: f64) {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let bits = v.to_bits();
    let mut buf = [0u8; 19];
    buf[0] = b'0';
    buf[1] = b'x';
    for i in 0..16 {
        buf[2 + i] = HEX[((bits >> (60 - 4 * i)) & 0xf) as usize];
    }
    buf[18] = b'\n';
    write(label);
    write(&buf);
}

/// A dense N x N double-precision matrix multiply, plus a reduction.
///
/// Sized so the whole thing runs in a few hundred million emulated steps in the fast build
/// and still finishes in reasonable time in the soft-float build, which is far slower.
const N: usize = 24;

static mut A: [[f64; N]; N] = [[0.0; N]; N];
static mut B: [[f64; N]; N] = [[0.0; N]; N];
static mut C: [[f64; N]; N] = [[0.0; N]; N];

#[no_mangle]
pub extern "C" fn _start() -> ! {
    // Operands that are not exactly representable, so every operation has to round and the
    // comparison is meaningful.
    unsafe {
        let mut seed: u64 = 0x2545_F491_4F6C_DD1D;
        for i in 0..N {
            for j in 0..N {
                seed = seed
                    .wrapping_mul(6364136223846793005)
                    .wrapping_add(1442695040888963407);
                A[i][j] = ((seed >> 33) & 0xFFFF) as f64 / 65536.0 - 0.5;
                seed = seed
                    .wrapping_mul(6364136223846793005)
                    .wrapping_add(1442695040888963407);
                B[i][j] = ((seed >> 33) & 0xFFFF) as f64 / 65536.0 - 0.5;
            }
        }

        // The GEMM. Repeated so that arithmetic, rather than start-up, dominates the trace.
        for _rep in 0..8 {
            for i in 0..N {
                for j in 0..N {
                    let mut s = 0.0f64;
                    for k in 0..N {
                        s += A[i][k] * B[k][j];
                    }
                    C[i][j] = s;
                }
            }
            // Feed the result back in so the repetitions cannot be optimised away.
            for i in 0..N {
                for j in 0..N {
                    A[i][j] = (A[i][j] + C[i][j] * 0.001) * 0.999;
                }
            }
        }

        let mut trace = 0.0f64;
        let mut frob = 0.0f64;
        for i in 0..N {
            trace += C[i][i];
            for j in 0..N {
                frob += C[i][j] * C[i][j];
            }
        }

        write_bits(b"trace     = ", trace);
        write_bits(b"frobenius = ", frob);
        write_bits(b"C[0][0]   = ", C[0][0]);
    }
    exit(0)
}
