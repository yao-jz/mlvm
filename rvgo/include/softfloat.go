package include

// for multi-thread version, you should keep the context for each thread

// Software floating-point underflow tininess-detection mode.
const (
	SoftfloatTininessBeforeRounding uint8 = 0
	SoftfloatTininessAfterRounding  uint8 = 1 // recommended default
)

// Software floating-point rounding mode.
const (
	Softfloat_round_near_even   uint8 = 0 // default
	Softfloat_round_minMag      uint8 = 1
	Softfloat_round_min         uint8 = 2
	Softfloat_round_max         uint8 = 3
	Softfloat_round_near_maxMag uint8 = 4
)

// Software floating-point exception flags.
//
// These deliberately match the RISC-V fflags bit assignment as well as Berkeley's, so no
// remapping is needed on the way into the CSR: NX=1, UF=2, OF=4, DZ=8, NV=16. Berkeley calls
// bit 3 "infinite" where RISC-V calls it "divide by zero"; they mean the same thing.
const (
	Softfloat_flag_inexact   uint8 = 1
	Softfloat_flag_underflow uint8 = 2
	Softfloat_flag_overflow  uint8 = 4
	Softfloat_flag_infinite  uint8 = 8
	Softfloat_flag_invalid   uint8 = 16
)

// Softfloat_exceptionFlags accumulates the exceptions raised by the arithmetic, and
// Softfloat_roundingMode selects how results are rounded.
//
// Package-level, as in the C reference, because threading a context through 119 ported files
// would obscure the correspondence with the original. That makes them **not safe for
// concurrent use**: exactly one VMState may be stepping at a time within a goroutine.
// fast/vm.go treats them as scratch - it resets the flags immediately before an operation and
// folds the result into the machine's fcsr immediately after - so nothing persists between
// instructions and no state leaks between VMs that run one after another.
var (
	Softfloat_exceptionFlags uint8
	Softfloat_roundingMode   uint8 = Softfloat_round_near_even
	Softfloat_detectTininess uint8 = SoftfloatTininessAfterRounding
)

// Softfloat_raiseFlags records an exception. The C reference makes this a function so a
// target can trap on it; here it only accumulates.
func Softfloat_raiseFlags(flags uint8) {
	Softfloat_exceptionFlags |= flags
}
