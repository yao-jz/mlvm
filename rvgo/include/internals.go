package include

const (
	Softfloat_mulAdd_subC    = 1
	Softfloat_mulAdd_subProd = 2
)

func SignF16UI(a uint16) bool {
	return (a >> 15) != 0
}

func ExpF16UI(a uint16) int8 {
	return int8((a >> 10) & 0x1F)
}

func FracF16UI(a uint16) uint16 {
	return a & 0x03FF
}

func PackToF16UI(sign bool, exp uint16, sig uint16) uint16 {
	signBit := uint16(0)
	if sign {
		signBit = 1
	}
	return (signBit << 15) + (exp << 10) + sig
}

func IsNaNF16UI(a uint16) bool {
	return ((^a & 0x7C00) == 0) && (a&0x03FF != 0)
}

type Exp8_Sig16 struct {
	exp int8
	sig uint16
}

func SignBF16UI(a uint16) bool {
	return (a >> 15) != 0
}

func ExpBF16UI(a uint16) int16 {
	return int16((a >> 7) & 0xFF)
}

func FracBF16UI(a uint16) uint16 {
	return a & 0x07F
}

func PackToBF16UI(sign bool, exp uint16, sig uint16) uint16 {
	signBit := uint16(0)
	if sign {
		signBit = 1
	}
	return (signBit << 15) + (exp << 7) + sig
}

func IsNaNBF16UI(a uint16) bool {
	return ((^a & 0x7FC0) == 0) && (a&0x07F != 0)
}

func SignF32UI(a uint32) bool {
	return (a >> 31) != 0
}

func ExpF32UI(a uint32) int16 {
	return int16((a >> 23) & 0xFF)
}

func FracF32UI(a uint32) uint32 {
	return a & 0x007FFFFF
}

func PackToF32UI(sign bool, exp uint32, sig uint32) uint32 {
	signBit := uint32(0)
	if sign {
		signBit = 1
	}
	return (signBit << 31) + (exp << 23) + sig
}

func IsNaNF32UI(a uint32) bool {
	return ((^a & 0x7F800000) == 0) && (a&0x007FFFFF != 0)
}

type Exp16_Sig32 struct {
	exp int16
	sig uint32
}

func SignF64UI(a uint64) bool {
	return (a >> 63) != 0
}

func ExpF64UI(a uint64) int16 {
	return int16((a >> 52) & 0x7FF)
}

func FracF64UI(a uint64) uint64 {
	return a & 0x000FFFFFFFFFFFFF
}

func PackToF64UI(sign bool, exp uint64, sig uint64) uint64 {
	signBit := uint64(0)
	if sign {
		signBit = 1
	}
	return (signBit << 63) + (exp << 52) + sig
}

func IsNaNF64UI(a uint64) bool {
	return ((^a & 0x7FF0000000000000) == 0) && (a&0x000FFFFFFFFFFFFF != 0)
}

type Exp16_Sig64 struct {
	exp int16
	sig uint64
}
