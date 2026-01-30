// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

import (
	"encoding/binary"
	"io"
)

// MPQ format constants
const (
	// Magic signature "MPQ\x1A" in little-endian
	mpqMagic = 0x1A51504D

	// Format versions
	formatVersion1 = 0 // Original format (up to 4GB)
	formatVersion2 = 1 // Extended format (Burning Crusade+)

	// Header sizes
	headerSizeV1 = 0x20 // 32 bytes
	headerSizeV2 = 0x2C // 44 bytes

	// Block table entry flags
	fileImplode      = 0x00000100 // Imploded (PKWARE compression)
	fileCompress     = 0x00000200 // Compressed (multi-algorithm)
	fileEncrypted    = 0x00010000 // Encrypted
	fileFixKey       = 0x00020000 // Key adjusted by block offset
	filePatchFile    = 0x00100000 // Patch file
	fileSingleUnit   = 0x01000000 // Single unit (not split into sectors)
	fileDeleteMarker = 0x02000000 // File is a deletion marker
	fileSectorCRC    = 0x04000000 // Sector CRC values after data
	fileExists       = 0x80000000 // File exists

	// Hash table entry constants
	hashTableEmpty   = 0xFFFFFFFF
	hashTableDeleted = 0xFFFFFFFE

	// Locale
	localeNeutral = 0x00000000

	// Default sector size (4096 bytes = 2^12)
	defaultSectorSizeShift = 12
	defaultSectorSize      = 1 << defaultSectorSizeShift
)

// baseHeader is the MPQ archive header (V1 format - 32 bytes)
type baseHeader struct {
	Magic            uint32 // "MPQ\x1A"
	HeaderSize       uint32 // Size of this header (0x20 for V1, 0x2C for V2)
	ArchiveSize      uint32 // Size of the entire archive (deprecated in V2)
	FormatVersion    uint16 // Format version (0 = V1, 1 = V2)
	SectorSizeShift  uint16 // Power of 2 for sector size
	HashTableOffset  uint32 // Offset to hash table (low 32 bits)
	BlockTableOffset uint32 // Offset to block table (low 32 bits)
	HashTableSize    uint32 // Number of entries in hash table
	BlockTableSize   uint32 // Number of entries in block table
}

// extendedHeader contains V2 extended header fields (12 bytes)
type extendedHeader struct {
	HiBlockTableOffset64 uint64 // 64-bit offset to the hi-block table
	HashTableOffsetHi    uint16 // High 16 bits of hash table offset
	BlockTableOffsetHi   uint16 // High 16 bits of block table offset
}

// archiveHeader combines V1 and V2 headers
type archiveHeader struct {
	baseHeader
	extendedHeader
}

// getHashTableOffset64 returns the full 64-bit hash table offset
func (h *archiveHeader) getHashTableOffset64() uint64 {
	if h.FormatVersion >= formatVersion2 {
		return uint64(h.HashTableOffset) | (uint64(h.HashTableOffsetHi) << 32)
	}
	return uint64(h.HashTableOffset)
}

// getBlockTableOffset64 returns the full 64-bit block table offset
func (h *archiveHeader) getBlockTableOffset64() uint64 {
	if h.FormatVersion >= formatVersion2 {
		return uint64(h.BlockTableOffset) | (uint64(h.BlockTableOffsetHi) << 32)
	}
	return uint64(h.BlockTableOffset)
}

// setHashTableOffset64 sets the hash table offset
func (h *archiveHeader) setHashTableOffset64(offset uint64) {
	h.HashTableOffset = uint32(offset)
	h.HashTableOffsetHi = uint16(offset >> 32)
}

// setBlockTableOffset64 sets the block table offset
func (h *archiveHeader) setBlockTableOffset64(offset uint64) {
	h.BlockTableOffset = uint32(offset)
	h.BlockTableOffsetHi = uint16(offset >> 32)
}

// hashTableEntry represents an entry in the hash table
type hashTableEntry struct {
	HashA      uint32 // First hash of the file name
	HashB      uint32 // Second hash of the file name
	Locale     uint16 // Locale ID
	Platform   uint16 // Platform ID (0 = default)
	BlockIndex uint32 // Index into the block table
}

// blockTableEntry represents an entry in the block table
type blockTableEntry struct {
	FilePos        uint32 // Offset of the file data (low 32 bits)
	CompressedSize uint32 // Compressed file size
	FileSize       uint32 // Uncompressed file size
	Flags          uint32 // File flags
}

// blockTableEntryEx extends blockTableEntry with 64-bit offset support
type blockTableEntryEx struct {
	blockTableEntry
	FilePosHi uint16 // High 16 bits of file offset (from extended block table)
}

// getFilePos64 returns the full 64-bit file position
func (b *blockTableEntryEx) getFilePos64() uint64 {
	return uint64(b.FilePos) | (uint64(b.FilePosHi) << 32)
}

// setFilePos64 sets the file position
func (b *blockTableEntryEx) setFilePos64(pos uint64) {
	b.FilePos = uint32(pos)
	b.FilePosHi = uint16(pos >> 32)
}

// readArchiveHeader reads the MPQ header from a reader
func readArchiveHeader(r io.ReadSeeker) (*archiveHeader, error) {
	h := &archiveHeader{}

	if err := binary.Read(r, binary.LittleEndian, &h.baseHeader); err != nil {
		return nil, err
	}

	if h.FormatVersion >= formatVersion2 && h.HeaderSize >= headerSizeV2 {
		if err := binary.Read(r, binary.LittleEndian, &h.extendedHeader); err != nil {
			return nil, err
		}
	}

	return h, nil
}

// writeArchiveHeader writes the MPQ header to a writer
func writeArchiveHeader(w io.Writer, h *archiveHeader) error {
	if err := binary.Write(w, binary.LittleEndian, &h.baseHeader); err != nil {
		return err
	}

	if h.FormatVersion >= formatVersion2 {
		if err := binary.Write(w, binary.LittleEndian, &h.extendedHeader); err != nil {
			return err
		}
	}

	return nil
}

// readUint32Array reads an array of uint32 values
func readUint32Array(r io.Reader, data []uint32) error {
	return binary.Read(r, binary.LittleEndian, data)
}

// readUint16Array reads an array of uint16 values
func readUint16Array(r io.Reader, data []uint16) error {
	return binary.Read(r, binary.LittleEndian, data)
}

// writeUint32Array writes an array of uint32 values
func writeUint32Array(w io.Writer, data []uint32) error {
	return binary.Write(w, binary.LittleEndian, data)
}

// writeUint16Array writes an array of uint16 values
func writeUint16Array(w io.Writer, data []uint16) error {
	return binary.Write(w, binary.LittleEndian, data)
}
