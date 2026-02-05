// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

import "encoding/binary"

const (
	attributesVersion = 100
	attributesFlagCRC32 = 0x00000001
)

type attributesWriter struct {
	crc32 []uint32
}

func newAttributesWriter(fileCount int) *attributesWriter {
	return &attributesWriter{
		crc32: make([]uint32, fileCount),
	}
}

func (a *attributesWriter) setEntry(index int, data []byte) {
	if index < 0 || index >= len(a.crc32) {
		return
	}
	if data == nil {
		// Set CRC32 to 0 (used for placeholder entries like attributes file itself)
		a.crc32[index] = 0
	} else {
		a.crc32[index] = crc32(data)
	}
}

func (a *attributesWriter) build() ([]byte, error) {
	if len(a.crc32) == 0 {
		return nil, nil
	}

	data := make([]byte, 8+len(a.crc32)*4)
	binary.LittleEndian.PutUint32(data[0:4], attributesVersion)
	binary.LittleEndian.PutUint32(data[4:8], attributesFlagCRC32)

	offset := 8
	for _, value := range a.crc32 {
		binary.LittleEndian.PutUint32(data[offset:offset+4], value)
		offset += 4
	}

	return data, nil
}
