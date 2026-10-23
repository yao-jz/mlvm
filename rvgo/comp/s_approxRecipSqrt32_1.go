package comp

func Softfloat_approxRecipSqrt32_1(oddExpA uint32, a uint32) uint32 {
	var index int
	var eps, r0 uint16
	var ESqrR0 uint32
	var sigma0 uint32
	var r uint32
	var sqrSigma0 uint32

	index = int((a >> 27 & 0xE) + oddExpA)
	eps = uint16(a >> 12)
	r0 = Softfloat_approxRecipSqrt_1k0s[index] - uint16((uint32(Softfloat_approxRecipSqrt_1k1s[index])*uint32(eps))>>20)
	ESqrR0 = uint32(r0) * uint32(r0)
	if oddExpA == 0 {
		ESqrR0 <<= 1
	}
	sigma0 = ^uint32((uint64(ESqrR0) * uint64(a)) >> 23)
	r = (uint32(r0) << 16) + uint32((uint64(r0)*uint64(sigma0))>>25)
	sqrSigma0 = uint32((uint64(sigma0) * uint64(sigma0)) >> 32)
	r += uint32(uint64(((r >> 1) + (r >> 3) - (uint32(r0) << 14))) * uint64(sqrSigma0) >> 48)
	if r&0x80000000 == 0 {
		r = 0x80000000
	}
	return r
}
