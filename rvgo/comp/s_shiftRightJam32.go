package comp

func Softfloat_shiftRightJam32(a uint32, dist uint16) uint32 {
	if dist < 31 {
		jam := uint32(0)
		if (a << (-dist & 31)) != 0 {
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
