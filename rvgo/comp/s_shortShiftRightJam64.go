package comp

func Softfloat_shortShiftRightJam64(a uint64, dist uint8) uint64 {
	// return a>>dist | ((a & (((uint_fast64_t) 1<<dist) - 1)) != 0);
	temp := uint64(0)
	if (a & ((uint64(1) << dist) - 1)) != 0 {
		temp = 1
	}
	return a>>dist | temp
}
