//! The float co-processor: one IEEE-754 operation, as RV64I instructions.
//!
//! `RISCV.sol` cannot execute `FADD.D` — settling one would need IEEE-754 in Yul. But the
//! same operation compiled for RV64IMA, where the compiler has no float instructions
//! available, becomes integer instructions that `RISCV.sol` *can* execute and has been
//! cross-checked on 84 conformance suites plus a full Go program.
//!
//! So a disputed float instruction is settled by nesting: derive a canonical nested machine
//! state from the operands, bisect its trace, and settle one of those steps with the
//! verifier that already exists. Measured, the whole nested trace is 115-178 instructions,
//! so the sub-game is 7 rounds. Cartesi does this for a whole machine and needs 2^20.
//!
//! ## The ABI is the protocol
//!
//! Both parties must derive *byte-identical* nested initial states or the sub-game is
//! meaningless. So this program takes no arguments, reads no environment and touches no
//! stack-passed data. Operands live at fixed absolute addresses, chosen clear of the ELF
//! image and of every other MLVM region. The nested pre-state is then a pure function of
//! (image, op, a, b, c) — which is what `fast.NewFloatSubGame` computes and what the
//! contract checks.
//!
//! ## This must match MLVM, not the standard
//!
//! The co-processor's job is to reproduce what `rvgo/fast` does, bit for bit. Where MLVM
//! departs from IEEE-754 or from RISC-V, this file departs with it, and says so — because
//! settling a computation the outer VM did not perform would lose the honest party a dispute
//! they were right about. Every such place is marked `MATCH MLVM:` below, and
//! `TestFloatSubGameMatchesSoftFloat` is what holds them together.
//!
//! Run:  ./scripts/float-dispute.sh

#![no_std]
#![no_main]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_: &PanicInfo) -> ! {
    exit(1)
}

/// Fixed I/O addresses. These are protocol constants: `fast/floatsubgame.go` and
/// `FloatSubGame.sol` hardcode the same values, and changing one without the others silently
/// breaks the sub-game. The region is unused by every other part of the address space — the
/// ELF image sits near 0, the heap at 0x7f<<40, the stack at 2^60, randomness at 0x1500…,
/// staged files at 0x2000…
///
/// The split at +32 is deliberate. MLVM's merkle leaves are 32 bytes, so the four inputs
/// occupy one leaf and the output occupies the next — and because `IO_BASE` is 64-byte
/// aligned, those two leaves are *siblings*. A single merkle branch therefore carries both:
/// the contract writes the inputs into one leaf and reads the result out of the other,
/// using the same 60 proof nodes.
const IO_BASE: usize = 0x3000_0000_0000_0000;
const ADDR_OP: *mut u64 = IO_BASE as *mut u64; // which operation
const ADDR_A: *mut u64 = (IO_BASE + 8) as *mut u64; // first operand, raw bits
const ADDR_B: *mut u64 = (IO_BASE + 16) as *mut u64; // second operand, raw bits
const ADDR_C: *mut u64 = (IO_BASE + 24) as *mut u64; // third operand, for the FMADD family
const ADDR_OUT: *mut u64 = (IO_BASE + 32) as *mut u64; // result, raw bits — the next leaf
const ADDR_FLAGS: *mut u64 = (IO_BASE + 40) as *mut u64; // IEEE-754 exceptions raised

// Operation selectors.
//
// The two-operand arithmetic keeps the RISC-V `funct5` values so the outer VM's decode
// passes straight through; the rest are MLVM's own, because RISC-V distinguishes them by
// opcode and by the `rs2` field rather than by `funct5` alone.
const OP_ADD: u64 = 0x00;
const OP_SUB: u64 = 0x01;
const OP_MUL: u64 = 0x02;
const OP_DIV: u64 = 0x03;
const OP_MIN: u64 = 0x05;
const OP_SQRT: u64 = 0x0B;
const OP_MAX: u64 = 0x15;

// Three-operand multiply-add family: RISC-V opcodes 0x43/0x47/0x4B/0x4F.
const OP_MADD: u64 = 0x20;
const OP_MSUB: u64 = 0x21;
const OP_NMSUB: u64 = 0x22;
const OP_NMADD: u64 = 0x23;

// Integer to double. RISC-V puts the variant in `rs2`.
const OP_CVT_D_W: u64 = 0x30;
const OP_CVT_D_WU: u64 = 0x31;
const OP_CVT_D_L: u64 = 0x32;
const OP_CVT_D_LU: u64 = 0x33;

// Double to integer.
const OP_CVT_W_D: u64 = 0x38;
const OP_CVT_WU_D: u64 = 0x39;
const OP_CVT_L_D: u64 = 0x3A;
const OP_CVT_LU_D: u64 = 0x3B;

// Comparisons. These write an integer register, so the result is 0 or 1, not float bits.
const OP_EQ: u64 = 0x40;
const OP_LT: u64 = 0x41;
const OP_LE: u64 = 0x42;

#[inline(always)]
unsafe fn syscall1(n: usize, a0: usize) -> usize {
    let ret: usize;
    core::arch::asm!(
        "ecall",
        in("a7") n,
        inlateout("a0") a0 => ret,
        options(nostack)
    );
    ret
}

fn exit(code: usize) -> ! {
    unsafe { syscall1(93, code) };
    loop {}
}

/// Prevent constant folding, and prevent the optimiser from fusing a multiply and an add.
///
/// Two jobs, both load-bearing. Without it the optimiser evaluates the operation at compile
/// time and the binary stores a literal — the instruction count then comes out identical for
/// every input, which is how the first attempt at measuring this silently produced nothing.
/// And in the multiply-add family it keeps `x * y + z` from contracting into a single fused
/// operation, which MLVM does *not* do (see `OP_MADD`). Laundering the value through an empty
/// asm block makes it opaque at the cost of one `mv`.
#[inline(always)]
fn opaque(v: u64) -> u64 {
    let out: u64;
    unsafe {
        core::arch::asm!("mv {0}, {1}", out(reg) out, in(reg) v, options(pure, nomem, nostack))
    };
    out
}

/// Repetitions, for measuring the marginal cost of one operation by differencing two builds
/// (`./scripts/float-nesting.sh`). The sub-game itself always runs with `OPS = 1`; chaining
/// only makes sense for the two-operand arithmetic.
const OPS: usize = 1;

#[no_mangle]
pub extern "C" fn _start() -> ! {
    unsafe {
        let op = core::ptr::read_volatile(ADDR_OP);
        let a = core::ptr::read_volatile(ADDR_A);
        let b = core::ptr::read_volatile(ADDR_B);
        let c = core::ptr::read_volatile(ADDR_C);

        let mut acc = a;
        let mut flags = 0u64;
        let mut i = 0;
        while i < OPS {
            let (v, f) = compute(op, opaque(acc), opaque(b), opaque(c));
            acc = v;
            flags |= f;
            i += 1;
        }

        core::ptr::write_volatile(ADDR_OUT, acc);
        core::ptr::write_volatile(ADDR_FLAGS, flags);
    }
    exit(0)
}

// IEEE-754 exception flags, in the RISC-V fflags bit order.
const NX: u64 = 1; // inexact
const UF: u64 = 2; // underflow
const OF: u64 = 4; // overflow
const DZ: u64 = 8; // divide by zero
const NV: u64 = 16; // invalid

// Raw bit-pattern predicates. Cheaper and clearer than going through f64 comparisons, and
// they work on the signaling/quiet distinction that f64 methods hide.
#[inline(always)]
fn is_nan(x: u64) -> bool {
    x & 0x7ff0_0000_0000_0000 == 0x7ff0_0000_0000_0000 && x & 0x000f_ffff_ffff_ffff != 0
}
#[inline(always)]
fn is_snan(x: u64) -> bool {
    is_nan(x) && x & 0x0008_0000_0000_0000 == 0
}
#[inline(always)]
fn is_inf(x: u64) -> bool {
    x & 0x7fff_ffff_ffff_ffff == 0x7ff0_0000_0000_0000
}
#[inline(always)]
fn is_zero(x: u64) -> bool {
    x & 0x7fff_ffff_ffff_ffff == 0
}
#[inline(always)]
fn is_neg(x: u64) -> bool {
    x >> 63 != 0
}

/// A finite double, exactly, as an integer significand times a power of two.
///
/// Every finite double is `m * 2^e` for integers m and e, and that representation is what
/// makes the exactness tests below simple: two such values are equal iff their normalised
/// (odd-significand) forms match. No shifting, so no overflow to guard against.
#[inline(always)]
fn decompose(x: u64) -> (u128, i32) {
    let biased = ((x >> 52) & 0x7ff) as i32;
    let frac = x & 0x000f_ffff_ffff_ffff;
    if biased == 0 {
        (frac as u128, -1074) // subnormal (and zero)
    } else {
        ((frac | (1 << 52)) as u128, biased - 1075)
    }
}

/// Strip trailing zeros so that `m * 2^e` has a unique representation.
#[inline(always)]
fn normalize(m: u128, e: i32) -> (u128, i32) {
    if m == 0 {
        return (0, 0);
    }
    let tz = m.trailing_zeros() as i32;
    (m >> tz, e + tz)
}

/// Whether `m1 * 2^e1` and `m2 * 2^e2` are the same number.
#[inline(always)]
fn same_value(m1: u128, e1: i32, m2: u128, e2: i32) -> bool {
    normalize(m1, e1) == normalize(m2, e2)
}

/// Was `a + b` (or `a - b`, with b negated) exact?
///
/// Knuth's 2Sum computes the rounding error exactly, using only the same soft-float the
/// result came from, and it is valid for every pair of finite doubles including subnormals -
/// the only requirement is round-to-nearest and no overflow. Both hold here, and an overflow
/// sets OF|NX through a different path anyway.
#[inline(always)]
fn add_is_inexact(x: f64, y: f64, s: f64) -> bool {
    let bb = f64::from_bits(opaque((s - x).to_bits()));
    let err = (x - f64::from_bits(opaque((s - bb).to_bits()))) + (y - bb);
    err != 0.0
}

fn compute(op: u64, a: u64, b: u64, c: u64) -> (u64, u64) {
    let x = f64::from_bits(a);
    let y = f64::from_bits(b);
    let _ = c;

    match op {
        OP_ADD => arith2(a, b, canonicalize((x + y).to_bits()), OP_ADD),
        OP_SUB => arith2(a, b, canonicalize((x - y).to_bits()), OP_SUB),
        OP_MUL => arith2(a, b, canonicalize((x * y).to_bits()), OP_MUL),
        OP_DIV => arith2(a, b, canonicalize((x / y).to_bits()), OP_DIV),
        OP_SQRT => arith2(a, a, canonicalize(libm::sqrt(x).to_bits()), OP_SQRT),

        // FMIN and FMAX are not a comparison, and three details make the difference:
        // NaN is absorbed rather than propagated (the *non*-NaN operand wins), only a
        // signaling NaN is invalid, and -0.0 is ordered below +0.0 even though they compare
        // equal. See comp/f64_minmax.go.
        OP_MIN | OP_MAX => {
            let nv = if is_snan(a) || is_snan(b) { NV } else { 0 };
            let r = if is_nan(a) && is_nan(b) {
                CANONICAL_NAN
            } else if is_nan(a) {
                b
            } else if is_nan(b) {
                a
            } else if is_zero(a) && is_zero(b) {
                // Equal in value, ordered by sign.
                let want_neg = op == OP_MIN;
                if is_neg(a) == want_neg { a } else { b }
            } else if (x < y) == (op == OP_MIN) {
                a
            } else {
                b
            };
            (r, nv)
        }

        // Genuinely fused: one rounding over the whole a*b+c, not two. RISC-V negates the
        // product for FNMSUB/FNMADD and the addend for FMSUB/FNMADD; doing it by flipping
        // sign bits keeps all four on one code path.
        OP_MADD => fma(a, b, c),
        OP_MSUB => fma(a, b, c ^ SIGN_BIT),
        OP_NMSUB => fma(a ^ SIGN_BIT, b, c),
        OP_NMADD => fma(a ^ SIGN_BIT, b, c ^ SIGN_BIT),

        // Integer to double. Exact unless the integer needs more than 53 significant bits.
        OP_CVT_D_W => cvt_to_double((a as u32 as i32) as i128),
        OP_CVT_D_WU => cvt_to_double((a as u32) as i128),
        OP_CVT_D_L => cvt_to_double((a as i64) as i128),
        OP_CVT_D_LU => cvt_to_double(a as i128),

        OP_CVT_W_D => cvt_to_int(a, 32, true),
        OP_CVT_WU_D => cvt_to_int(a, 32, false),
        OP_CVT_L_D => cvt_to_int(a, 64, true),
        OP_CVT_LU_D => cvt_to_int(a, 64, false),

        // Comparisons write an integer register: 0 or 1, never float bits, so no
        // canonicalisation and never inexact. FEQ is a *quiet* comparison - only a signaling
        // NaN is invalid - while FLT and FLE are signaling, so any NaN is.
        OP_EQ => ((x == y) as u64, if is_snan(a) || is_snan(b) { NV } else { 0 }),
        OP_LT => ((x < y) as u64, if is_nan(a) || is_nan(b) { NV } else { 0 }),
        OP_LE => ((x <= y) as u64, if is_nan(a) || is_nan(b) { NV } else { 0 }),

        // An unknown selector must not quietly become addition. The contract validates `op`
        // before deriving the pre-state, but if the two lists ever drift apart this fails
        // the sub-game rather than settling the wrong operation.
        _ => exit(2),
    }
}

/// Flags for a two-operand arithmetic operation whose result is already known.
///
/// Knowing the correctly-rounded result is what makes this cheap: the question is only
/// "was it exact?", which is an integer predicate on the significands, not another round of
/// IEEE-754 arithmetic.
fn arith2(a: u64, b: u64, r: u64, op: u64) -> (u64, u64) {
    // A NaN operand short-circuits everything: the result is the canonical NaN, and the only
    // flag is invalid, and only for a signaling NaN.
    if is_nan(a) || is_nan(b) {
        return (r, if is_snan(a) || is_snan(b) { NV } else { 0 });
    }

    // Operations that are invalid on their face, before any arithmetic happens.
    let invalid = match op {
        OP_ADD => is_inf(a) && is_inf(b) && is_neg(a) != is_neg(b),
        OP_SUB => is_inf(a) && is_inf(b) && is_neg(a) == is_neg(b),
        OP_MUL => (is_zero(a) && is_inf(b)) || (is_inf(a) && is_zero(b)),
        OP_DIV => (is_zero(a) && is_zero(b)) || (is_inf(a) && is_inf(b)),
        OP_SQRT => is_neg(a) && !is_zero(a),
        _ => false,
    };
    if invalid {
        return (r, NV);
    }

    // An infinite operand makes the result infinite exactly, with no rounding. This has to
    // come before the divide-by-zero test: IEEE-754 raises divideByZero only when an exact
    // infinite result comes from *finite* operands, so inf/0 is plain infinity and raises
    // nothing.
    if is_inf(a) || is_inf(b) {
        return (r, 0);
    }

    // Division by zero. Reached only when the numerator is finite and non-zero, because the
    // NaN, 0/0, inf/inf and infinite-operand cases already returned.
    if op == OP_DIV && is_zero(b) {
        return (r, DZ);
    }

    // Overflow: a finite computation that produced infinity. Berkeley raises inexact with it.
    if is_inf(r) {
        return (r, OF | NX);
    }

    let inexact = match op {
        OP_ADD => add_is_inexact(f64::from_bits(a), f64::from_bits(b), f64::from_bits(r)),
        OP_SUB => add_is_inexact(f64::from_bits(a), -f64::from_bits(b), f64::from_bits(r)),
        OP_MUL => {
            let (ma, ea) = decompose(a);
            let (mb, eb) = decompose(b);
            let (mr, er) = decompose(r);
            !same_value(ma * mb, ea + eb, mr, er)
        }
        // a / b is exact iff a == r * b exactly.
        OP_DIV => {
            let (ma, ea) = decompose(a);
            let (mb, eb) = decompose(b);
            let (mr, er) = decompose(r);
            !same_value(ma, ea, mr * mb, er + eb)
        }
        // sqrt(a) is exact iff a == r * r exactly.
        OP_SQRT => {
            let (ma, ea) = decompose(a);
            let (mr, er) = decompose(r);
            !same_value(ma, ea, mr * mr, er + er)
        }
        _ => false,
    };
    if !inexact {
        return (r, 0);
    }

    // Underflow, with tininess detected after rounding: a result that landed in the
    // subnormal range (or at zero) having lost bits on the way.
    let tiny = (r >> 52) & 0x7ff == 0;
    (r, NX | if tiny { UF } else { 0 })
}

/// The sign bit of a double, flipped to negate an operand.
const SIGN_BIT: u64 = 0x8000_0000_0000_0000;

/// Fused multiply-add: `a*b + c` with a single rounding.
///
/// The value comes from `libm::fma`, which is correctly rounded. The flags are the harder
/// half, because the exact `a*b + c` does not fit in a double and so cannot be compared
/// against the result directly.
///
/// The trick is to ask the question the other way round:
///
/// ```text
///     a*b + c == r    ⟺    a*b == r - c
/// ```
///
/// `r - c` is recovered *exactly* by 2Sum as an unevaluated pair, and `a*b` is an exact
/// 106-bit integer product of the significands. So both sides are exact integers times a
/// power of two, and the comparison is integer arithmetic.
///
/// Doing it this way avoids ever computing `fl(a*b)`, which matters: the more obvious route
/// through `twoProduct` — `pl = fma(a, b, -fl(a*b))` — silently returns zero when the
/// product underflows, so it reports `FMADD(5e-324, 0.5, 1.5)` as exact when it is not.
fn fma(a: u64, b: u64, c: u64) -> (u64, u64) {
    let (x, y, z) = (f64::from_bits(a), f64::from_bits(b), f64::from_bits(c));
    let r = canonicalize(libm::fma(x, y, z).to_bits());

    // NaN operands settle everything: canonical NaN out, invalid only for a signalling NaN.
    if is_nan(a) || is_nan(b) || is_nan(c) {
        return (r, if is_snan(a) || is_snan(b) || is_snan(c) { NV } else { 0 });
    }
    // An infinite product with a zero factor is invalid, and so is an infinite product added
    // to the opposite infinity.
    if (is_inf(a) && is_zero(b)) || (is_inf(b) && is_zero(a)) {
        return (r, NV);
    }
    let prod_inf = is_inf(a) || is_inf(b);
    if prod_inf && is_inf(c) && (is_neg(a) != is_neg(b)) != is_neg(c) {
        return (r, NV);
    }
    // Any infinity left makes the result infinite exactly, with no rounding.
    if prod_inf || is_inf(c) {
        return (r, 0);
    }
    if is_inf(r) {
        return (r, OF | NX);
    }

    // The exact product, as a signed integer times a power of two.
    let (ma, ea) = decompose(a);
    let (mb, eb) = decompose(b);
    let prod_neg = is_neg(a) != is_neg(b);
    let (prod_mag, prod_exp) = normalize(ma * mb, ea + eb);

    // r - c, exactly, as an unevaluated pair.
    //
    // The subtraction can overflow when r and c are near the top of the range with opposite
    // signs, and halving both first fixes that — but only do it when it is needed. Halving
    // unconditionally destroys subnormal operands: `5e-324 * 0.5` underflows to zero, which
    // silently reported `FMADD(1, 1, 5e-324)` as exact.
    let (mut dh, mut dl) = two_sum(f64::from_bits(r), -z);
    let mut scale = 0;
    if !dh.is_finite() {
        let (h, l) = two_sum(f64::from_bits(r) * 0.5, -(z * 0.5));
        dh = h;
        dl = l;
        scale = 1;
    }

    let exact = same_as_pair(prod_mag, prod_exp - scale, prod_neg, dh, dl);
    if exact {
        return (r, 0);
    }
    let tiny = (r >> 52) & 0x7ff == 0;
    (r, NX | if tiny { UF } else { 0 })
}

/// Whether `±mag · 2^exp` equals `dh + dl` exactly.
///
/// `dh` and `dl` come from a 2Sum, so the pair is an exact unevaluated sum. Both sides are
/// integers times powers of two, so this is an integer comparison — with one bound that keeps
/// it inside 128 bits: a 106-bit product cannot equal a value spanning more significant bits
/// than that, so anything wider is unequal without needing to be computed.
fn same_as_pair(mag: u128, exp: i32, neg: bool, dh: f64, dl: f64) -> bool {
    let (dhb, dlb) = (dh.to_bits(), dl.to_bits());
    let (mh, eh) = { let (m, e) = decompose(dhb); normalize(m, e) };
    let (ml, el) = { let (m, e) = decompose(dlb); normalize(m, e) };

    if ml == 0 {
        if mh == 0 {
            return mag == 0;
        }
        return mag == mh && exp == eh && neg == is_neg(dhb);
    }
    if mh == 0 {
        return mag == ml && exp == el && neg == is_neg(dlb);
    }

    // Align to the lower exponent. The shifted significand stays under 2^107 because the
    // span check below caps `bit_len + shift`, which is exactly what would overflow.
    let lo = if eh <= el { eh } else { el };
    let top_h = eh + bit_len(mh);
    let top_l = el + bit_len(ml);
    let top = if top_h >= top_l { top_h } else { top_l };
    if top - lo > 107 {
        return false;
    }

    let signed = |v: u128, negative: bool| -> i128 {
        if negative {
            -(v as i128)
        } else {
            v as i128
        }
    };
    let sum = signed(mh << (eh - lo), is_neg(dhb)) + signed(ml << (el - lo), is_neg(dlb));
    if sum == 0 {
        return mag == 0;
    }
    let (sum_mag, sum_neg) = if sum < 0 { ((-sum) as u128, true) } else { (sum as u128, false) };
    let (sm, se) = normalize(sum_mag, lo);
    mag == sm && exp == se && neg == sum_neg
}

/// Number of significant bits in a non-zero magnitude.
#[inline(always)]
fn bit_len(m: u128) -> i32 {
    128 - m.leading_zeros() as i32
}

/// Knuth's 2Sum: the sum and its exact rounding error, for any two finite doubles.
#[inline(always)]
fn two_sum(x: f64, y: f64) -> (f64, f64) {
    let s = x + y;
    let bb = f64::from_bits(opaque((s - x).to_bits()));
    let err = (x - f64::from_bits(opaque((s - bb).to_bits()))) + (y - bb);
    (s, err)
}

/// Integer to double: exact unless the value needs more than 53 significant bits.
fn cvt_to_double(v: i128) -> (u64, u64) {
    let r = (v as f64).to_bits();
    if v == 0 {
        return (r, 0);
    }
    let neg = v < 0;
    let mag = if neg { (-v) as u128 } else { v as u128 };
    let (mr, er) = decompose(r);
    let exact = same_value(mag, 0, mr, er);
    (r, if exact { 0 } else { NX })
}

/// Double to integer.
///
/// MLVM rounds to nearest with ties to even here rather than truncating, raises inexact when
/// the value had a fractional part, and raises invalid - *instead* of inexact - when the
/// value is out of range or NaN, returning the saturated result.
fn cvt_to_int(a: u64, width: u32, signed: bool) -> (u64, u64) {
    if is_nan(a) {
        let v: u64 = match (width, signed) {
            (32, true) => i32::MAX as i64 as u64,
            (32, false) => u32::MAX as i32 as i64 as u64,
            (_, true) => i64::MAX as u64,
            (_, false) => u64::MAX,
        };
        return (v, NV);
    }

    let x = f64::from_bits(a);
    let rounded = libm::rint(x); // round to nearest, ties to even - what MLVM does

    // Out of range saturates, and that is an invalid operation rather than an inexact one.
    let (lo, hi): (f64, f64) = match (width, signed) {
        (32, true) => (-2147483648.0, 2147483647.0),
        (32, false) => (0.0, 4294967295.0),
        (_, true) => (-9223372036854775808.0, 9223372036854775807.0),
        (_, false) => (0.0, 18446744073709551615.0),
    };
    // Compared against the *rounded* value: MLVM rounds first, then checks range.
    if rounded < lo || rounded > hi {
        let v: u64 = match (width, signed) {
            (32, true) => {
                if is_neg(a) {
                    i32::MIN as i64 as u64
                } else {
                    i32::MAX as i64 as u64
                }
            }
            (32, false) => {
                if is_neg(a) {
                    0
                } else {
                    u32::MAX as i32 as i64 as u64
                }
            }
            (_, true) => {
                if is_neg(a) {
                    i64::MIN as u64
                } else {
                    i64::MAX as u64
                }
            }
            (_, false) => {
                if is_neg(a) {
                    0
                } else {
                    u64::MAX
                }
            }
        };
        return (v, NV);
    }

    let v: u64 = match (width, signed) {
        (32, true) => (rounded as i32) as i64 as u64,
        (32, false) => (rounded as u32) as i32 as i64 as u64,
        (_, true) => rounded as i64 as u64,
        (_, false) => rounded as u64,
    };
    // Inexact iff the original had a fractional part.
    (v, if rounded == x { 0 } else { NX })
}

/// RISC-V's canonical NaN.
const CANONICAL_NAN: u64 = 0x7ff8_0000_0000_0000;

/// Force every NaN result to the canonical NaN.
///
/// This is a real difference between two correct implementations, and it has to be resolved
/// here or the sub-game settles the wrong bits. IEEE-754 lets an implementation propagate an
/// input NaN's payload, and that is what Rust's `compiler_builtins` does — a signaling NaN
/// `0x7ff0…001` comes back quieted as `0x7ff8…001`. The RISC-V spec is stricter: §21.3 "NaN
/// Generation and Propagation" requires the *canonical* NaN for any operation producing NaN,
/// which is what MLVM's `rvgo/comp` implements.
///
/// Without this the two disagreed on 48 of the 1,522 differential vectors — every one of them
/// a signaling-NaN input. Canonicalising costs a handful of integer instructions, all of
/// which `RISCV.sol` executes.
#[inline(always)]
fn canonicalize(bits: u64) -> u64 {
    let is_nan = bits & 0x7ff0_0000_0000_0000 == 0x7ff0_0000_0000_0000
        && bits & 0x000f_ffff_ffff_ffff != 0;
    if is_nan {
        CANONICAL_NAN
    } else {
        bits
    }
}
