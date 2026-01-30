// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

// adler32 computes Adler-32 checksum (used for MPQ sector CRCs).
func adler32(data []byte) uint32 {
	const mod = 65521
	var a uint32 = 1
	var b uint32
	for _, v := range data {
		a = (a + uint32(v)) % mod
		b = (b + a) % mod
	}
	return (b << 16) | a
}
