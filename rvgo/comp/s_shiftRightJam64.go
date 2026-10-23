package comp

func Softfloat_shiftRightJam64(a uint64, dist uint32) uint64 {
	if dist < 63 {
		jam := uint64(0)
		if (a << (-dist & 63)) != 0 {
			jam = 1
		}
		return a>>dist | jam
	} else {
		if a != 0 {
			return 1
		}
	}
	return 0
}
