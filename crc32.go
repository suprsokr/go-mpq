// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

var crc32Table = func() [256]uint32 {
	var table [256]uint32
	const poly = 0xEDB88320
	for i := 0; i < 256; i++ {
		crc := uint32(i)
		for j := 0; j < 8; j++ {
			if crc&1 == 1 {
				crc = (crc >> 1) ^ poly
			} else {
				crc >>= 1
			}
		}
		table[i] = crc
	}
	return table
}()

func crc32(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, v := range data {
		crc = crc32Table[(crc^uint32(v))&0xFF] ^ (crc >> 8)
	}
	return ^crc
}
