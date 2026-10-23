package comp

func Softfloat_approxRecip32_1(a uint32) uint32 {
	var index int
	var eps, r0 uint16
	var sigma0 uint32
	var r uint32
	var sqrSigma0 uint32

	index = int((a >> 27) & 0xF)
	eps = uint16(a >> 11)
	r0 = Softfloat_approxRecip_1k0s[index] - uint16((uint32(Softfloat_approxRecip_1k1s[index])*uint32(eps))>>20)
	sigma0 = ^uint32((uint64(r0) * uint64(a)) >> 7)
	r = (uint32(r0) << 16) + uint32((uint64(r0)*uint64(sigma0))>>24)
	sqrSigma0 = uint32((uint64(sigma0) * uint64(sigma0)) >> 32)
	r += uint32((uint64(r) * uint64(sqrSigma0)) >> 48)
	return r
}
