package maps

import stdmath "math"

// MortonHash computes a 64-bit Morton code (z-order curve) for a tile.
func MortonHash(lat, lon, tileSize float64) uint64 {
	latIdx := int64(stdmath.Floor(lat / tileSize))
	lonIdx := int64(stdmath.Floor(lon / tileSize))
	return interleaveBits(uint32(latIdx), uint32(lonIdx))
}

func interleaveBits(x, y uint32) uint64 {
	var result uint64
	for i := 0; i < 32; i++ {
		result |= uint64((x>>i)&1) << (2 * i)
		result |= uint64((y>>i)&1) << (2*i + 1)
	}
	return result
}
