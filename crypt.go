// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

// Hash types for the hash function
const (
	hashTypeTableOffset = 0
	hashTypeNameA       = 1
	hashTypeNameB       = 2
	hashTypeFileKey     = 3
)

// cryptTable is the encryption/hash lookup table
var cryptTable [0x500]uint32

func init() {
	// Initialize the encryption table using the standard MPQ algorithm
	seed := uint32(0x00100001)

	for index1 := 0; index1 < 0x100; index1++ {
		index2 := index1
		for i := 0; i < 5; i++ {
			seed = (seed*125 + 3) % 0x2AAAAB
			temp1 := (seed & 0xFFFF) << 0x10

			seed = (seed*125 + 3) % 0x2AAAAB
			temp2 := seed & 0xFFFF

			cryptTable[index2] = temp1 | temp2
			index2 += 0x100
		}
	}
}

// hashString computes the MPQ hash of a string
func hashString(s string, hashType uint32) uint32 {
	seed1 := uint32(0x7FED7FED)
	seed2 := uint32(0xEEEEEEEE)

	for i := 0; i < len(s); i++ {
		ch := uint32(s[i])
		// Convert to uppercase
		if ch >= 'a' && ch <= 'z' {
			ch -= 0x20
		}
		// Convert forward slashes to backslashes
		if ch == '/' {
			ch = '\\'
		}

		seed1 = cryptTable[hashType*0x100+ch] ^ (seed1 + seed2)
		seed2 = ch + seed1 + seed2 + (seed2 << 5) + 3
	}

	return seed1
}

// encryptBlock encrypts a block of data in place
func encryptBlock(data []uint32, key uint32) {
	seed := uint32(0xEEEEEEEE)

	for i := range data {
		seed += cryptTable[0x400+(key&0xFF)]
		plain := data[i]
		encrypted := plain ^ (key + seed)
		key = ((^key << 0x15) + 0x11111111) | (key >> 0x0B)
		seed = plain + seed + (seed << 5) + 3
		data[i] = encrypted
	}
}

// decryptBlock decrypts a block of data in place
func decryptBlock(data []uint32, key uint32) {
	seed := uint32(0xEEEEEEEE)

	for i := range data {
		seed += cryptTable[0x400+(key&0xFF)]
		encrypted := data[i]
		plain := encrypted ^ (key + seed)
		key = ((^key << 0x15) + 0x11111111) | (key >> 0x0B)
		seed = plain + seed + (seed << 5) + 3
		data[i] = plain
	}
}

// decryptBytes decrypts a byte slice in place
// The data length must be a multiple of 4
func decryptBytes(data []byte, key uint32) {
	if len(data)%4 != 0 {
		// Pad to 4-byte boundary for decryption
		padded := make([]byte, (len(data)+3)&^3)
		copy(padded, data)
		data = padded
	}

	// Convert to uint32 slice for decryption
	words := make([]uint32, len(data)/4)
	for i := range words {
		words[i] = uint32(data[i*4]) |
			uint32(data[i*4+1])<<8 |
			uint32(data[i*4+2])<<16 |
			uint32(data[i*4+3])<<24
	}

	decryptBlock(words, key)

	// Convert back to bytes
	for i := range words {
		data[i*4] = byte(words[i])
		data[i*4+1] = byte(words[i] >> 8)
		data[i*4+2] = byte(words[i] >> 16)
		data[i*4+3] = byte(words[i] >> 24)
	}
}

// getFileKey computes the encryption key for a file
// based on its filename and block offset
func getFileKey(filename string, blockOffset uint64, fileSize uint32, flags uint32) uint32 {
	// Extract just the filename (without path)
	plainName := filename
	if idx := lastIndexOfSlash(filename); idx >= 0 {
		plainName = filename[idx+1:]
	}

	// Compute base key from filename
	key := hashString(plainName, hashTypeFileKey)

	// If the file uses adjusted key (MPQ_FILE_FIX_KEY / fileFixKey),
	// modify the key based on block offset and file size
	if flags&fileFixKey != 0 {
		key = (key + uint32(blockOffset)) ^ fileSize
	}

	return key
}

// lastIndexOfSlash finds the last path separator in a string
func lastIndexOfSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '\\' || s[i] == '/' {
			return i
		}
	}
	return -1
}
