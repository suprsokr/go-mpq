// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

import (
	"fmt"
	"os"
)

// writeArchive writes the complete MPQ archive
func (a *Archive) writeArchive() error {
	file, err := os.Create(a.tempPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer file.Close()

	// Initialize hash table with empty entries
	for i := range a.hashTable {
		a.hashTable[i] = hashTableEntry{
			HashA:      0xFFFFFFFF,
			HashB:      0xFFFFFFFF,
			Locale:     0xFFFF,
			Platform:   0xFFFF,
			BlockIndex: hashTableEmpty,
		}
	}

	// Reserve space for header
	headerSize := a.header.HeaderSize
	if _, err := file.Seek(int64(headerSize), 0); err != nil {
		return fmt.Errorf("seek past header: %w", err)
	}

	// Write file data and build block table
	// First pass: count actual files (excluding deletion markers might have no data)
	actualFileCount := 0
	for range a.pendingFiles {
		actualFileCount++
	}
	
	// Calculate total block count including special files
	// We'll add: (listfile) and (attributes) if they exist
	totalBlockCount := actualFileCount
	if actualFileCount > 0 {
		totalBlockCount++ // (listfile)
		totalBlockCount++ // (attributes)
	}
	
	a.blockTable = make([]blockTableEntryEx, 0, totalBlockCount)
	listFileContent := ""
	// Attributes file must include entries for ALL files in block table
	attributes := newAttributesWriter(totalBlockCount)
	needsHiBlockTable := false

	for i, pf := range a.pendingFiles {
		filePos, err := file.Seek(0, 1)
		if err != nil {
			return fmt.Errorf("get file position: %w", err)
		}

		if filePos > 0xFFFFFFFF {
			needsHiBlockTable = true
		}

		var dataToWrite []byte
		var flags uint32 = fileExists
		var compressedSize uint32

		// Handle deletion markers (no data)
		if pf.isDeleteMarker {
			flags = fileDeleteMarker | fileExists
			compressedSize = 0

			blockEntry := blockTableEntryEx{
				blockTableEntry: blockTableEntry{
					FilePos:        uint32(filePos),
					CompressedSize: 0,
					FileSize:       0,
					Flags:          flags,
				},
				FilePosHi: uint16(filePos >> 32),
			}
			a.blockTable = append(a.blockTable, blockEntry)

			if err := a.addToHashTable(pf.mpqPath, uint32(len(a.blockTable)-1)); err != nil {
				return fmt.Errorf("add to hash table: %w", err)
			}
			listFileContent += pf.mpqPath + "\r\n"
			continue
		}

		// Determine if we should use sectors or single-unit
		useSectors := len(pf.data) > int(a.sectorSize)*2 // Use sectors for larger files
		useSectorCRC := pf.generateCRC

		if useSectors {
			// Sector-based file with optional CRC
			dataToWrite, compressedSize, err = a.writeSectoredFile(pf.data, useSectorCRC)
			if err != nil {
				return fmt.Errorf("write sectored file %s: %w", pf.mpqPath, err)
			}
			flags |= fileCompress
			if useSectorCRC {
				flags |= fileSectorCRC
			}
		} else {
			// Single-unit file
			compressedData, err := compressData(pf.data)
			if err != nil {
				return fmt.Errorf("compress file %s: %w", pf.mpqPath, err)
			}

			flags |= fileSingleUnit

			if len(compressedData) < len(pf.data) {
				dataToWrite = compressedData
				flags |= fileCompress
			} else {
				dataToWrite = pf.data
			}

			// Add single-unit CRC if requested
			if useSectorCRC {
				crc := adler32(dataToWrite)
				crcBytes := make([]byte, 4)
				crcBytes[0] = byte(crc)
				crcBytes[1] = byte(crc >> 8)
				crcBytes[2] = byte(crc >> 16)
				crcBytes[3] = byte(crc >> 24)
				dataToWrite = append(dataToWrite, crcBytes...)
				flags |= fileSectorCRC
			}

			compressedSize = uint32(len(dataToWrite))
		}

		// Mark as patch file if requested
		if pf.isPatchFile {
			flags |= filePatchFile
		}

		if _, err := file.Write(dataToWrite); err != nil {
			return fmt.Errorf("write file data: %w", err)
		}

		// Add to block table
		blockEntry := blockTableEntryEx{
			blockTableEntry: blockTableEntry{
				FilePos:        uint32(filePos),
				CompressedSize: compressedSize,
				FileSize:       uint32(len(pf.data)),
				Flags:          flags,
			},
			FilePosHi: uint16(filePos >> 32),
		}
		a.blockTable = append(a.blockTable, blockEntry)
		attributes.setEntry(i, pf.data)

		// Add to hash table
		if err := a.addToHashTable(pf.mpqPath, uint32(len(a.blockTable)-1)); err != nil {
			return fmt.Errorf("add to hash table: %w", err)
		}

		listFileContent += pf.mpqPath + "\r\n"
	}

	// Add (listfile)
	if listFileContent != "" {
		listFileData := []byte(listFileContent)
		listFilePos, _ := file.Seek(0, 1)

		if listFilePos > 0xFFFFFFFF {
			needsHiBlockTable = true
		}

		compressedListFile, err := compressData(listFileData)
		if err != nil {
			return fmt.Errorf("compress listfile: %w", err)
		}

		var dataToWrite []byte
		var flags uint32 = fileExists | fileSingleUnit

		if len(compressedListFile) < len(listFileData) {
			dataToWrite = compressedListFile
			flags |= fileCompress
		} else {
			dataToWrite = listFileData
		}

		if _, err := file.Write(dataToWrite); err != nil {
			return fmt.Errorf("write listfile: %w", err)
		}

		blockEntry := blockTableEntryEx{
			blockTableEntry: blockTableEntry{
				FilePos:        uint32(listFilePos),
				CompressedSize: uint32(len(dataToWrite)),
				FileSize:       uint32(len(listFileData)),
				Flags:          flags,
			},
			FilePosHi: uint16(listFilePos >> 32),
		}
		a.blockTable = append(a.blockTable, blockEntry)

		// Add attributes entry for (listfile) - use index after user files
		listFileIndex := len(a.pendingFiles)
		attributes.setEntry(listFileIndex, listFileData)

		if err := a.addToHashTable("(listfile)", uint32(len(a.blockTable)-1)); err != nil {
			return fmt.Errorf("add listfile to hash table: %w", err)
		}
	}

	// Add (attributes)
	// Calculate attributes index for the (attributes) file itself
	attributesIndex := len(a.pendingFiles)
	if listFileContent != "" {
		attributesIndex++ // Account for (listfile)
	}
	// Set CRC32 to 0 for the (attributes) file entry (standard practice)
	attributes.setEntry(attributesIndex, nil)

	// Build attributes with all entries (including (attributes) file with CRC32=0)
	attributesData, err := attributes.build()
	if err != nil {
		return fmt.Errorf("build attributes: %w", err)
	}
	if len(attributesData) > 0 {
		attrPos, _ := file.Seek(0, 1)
		if attrPos > 0xFFFFFFFF {
			needsHiBlockTable = true
		}

		compressedAttributes, err := compressData(attributesData)
		if err != nil {
			return fmt.Errorf("compress attributes: %w", err)
		}

		var attrToWrite []byte
		var attrFlags uint32 = fileExists | fileSingleUnit
		if len(compressedAttributes) < len(attributesData) {
			attrToWrite = compressedAttributes
			attrFlags |= fileCompress
		} else {
			attrToWrite = attributesData
		}

		if _, err := file.Write(attrToWrite); err != nil {
			return fmt.Errorf("write attributes: %w", err)
		}

		blockEntry := blockTableEntryEx{
			blockTableEntry: blockTableEntry{
				FilePos:        uint32(attrPos),
				CompressedSize: uint32(len(attrToWrite)),
				FileSize:       uint32(len(attributesData)),
				Flags:          attrFlags,
			},
			FilePosHi: uint16(attrPos >> 32),
		}
		a.blockTable = append(a.blockTable, blockEntry)

		if err := a.addToHashTable("(attributes)", uint32(len(a.blockTable)-1)); err != nil {
			return fmt.Errorf("add attributes to hash table: %w", err)
		}
	}

	// Write hash table
	hashTableOffset, _ := file.Seek(0, 1)

	hashTableData := make([]uint32, len(a.hashTable)*4)
	for i, entry := range a.hashTable {
		hashTableData[i*4] = entry.HashA
		hashTableData[i*4+1] = entry.HashB
		hashTableData[i*4+2] = uint32(entry.Locale) | (uint32(entry.Platform) << 16)
		hashTableData[i*4+3] = entry.BlockIndex
	}
	encryptBlock(hashTableData, hashString("(hash table)", hashTypeFileKey))

	if err := writeUint32Array(file, hashTableData); err != nil {
		return fmt.Errorf("write hash table: %w", err)
	}

	// Write block table
	blockTableOffset, _ := file.Seek(0, 1)

	blockTableData := make([]uint32, len(a.blockTable)*4)
	for i, entry := range a.blockTable {
		blockTableData[i*4] = entry.FilePos
		blockTableData[i*4+1] = entry.CompressedSize
		blockTableData[i*4+2] = entry.FileSize
		blockTableData[i*4+3] = entry.Flags
	}
	encryptBlock(blockTableData, hashString("(block table)", hashTypeFileKey))

	if err := writeUint32Array(file, blockTableData); err != nil {
		return fmt.Errorf("write block table: %w", err)
	}

	// Write hi-block table if V2 and needed
	var hiBlockTableOffset int64
	if a.formatVersion == FormatV2 && needsHiBlockTable {
		hiBlockTableOffset, _ = file.Seek(0, 1)

		hiBlockTable := make([]uint16, len(a.blockTable))
		for i, entry := range a.blockTable {
			hiBlockTable[i] = entry.FilePosHi
		}

		if err := writeUint16Array(file, hiBlockTable); err != nil {
			return fmt.Errorf("write hi-block table: %w", err)
		}
	}

	// Get archive size (total file size from start of header)
	totalFileSize, _ := file.Seek(0, 1)
	
	// Archive size in header should be the size of the archive data section
	// (everything after the header), not the total file size.
	// This is what warcraft-rs expects for validation.
	archiveSize := uint32(totalFileSize) - a.header.HeaderSize

	// Update header
	// Note: Offsets are relative to archive start (including header), which matches
	// how warcraft-rs interprets them: archive_offset + header.get_hash_table_pos()
	a.header.setHashTableOffset64(uint64(hashTableOffset))
	a.header.setBlockTableOffset64(uint64(blockTableOffset))
	a.header.BlockTableSize = uint32(len(a.blockTable))
	a.header.ArchiveSize = archiveSize

	if a.formatVersion == FormatV2 {
		if needsHiBlockTable {
			a.header.HiBlockTableOffset64 = uint64(hiBlockTableOffset)
		} else {
			a.header.HiBlockTableOffset64 = 0
		}
	}

	// Write header
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("seek to header: %w", err)
	}

	if err := writeArchiveHeader(file, a.header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	return nil
}

// writeSectoredFile writes file data in sectors with optional CRC table.
// Returns the complete data buffer, its size, and any error.
func (a *Archive) writeSectoredFile(data []byte, useCRC bool) ([]byte, uint32, error) {
	numSectors := (uint32(len(data)) + a.sectorSize - 1) / a.sectorSize

	// Build sector offset table
	offsetTable := make([]uint32, numSectors+1)
	sectorCRCs := make([]uint32, 0, numSectors)
	sectors := make([][]byte, numSectors)

	// Calculate offset table size
	offsetTableSize := (numSectors + 1) * 4
	var crcTableSize uint32
	if useCRC {
		crcTableSize = numSectors * 4
	}

	// First offset points after offset table + CRC table
	currentOffset := offsetTableSize + crcTableSize

	// Compress each sector
	for i := uint32(0); i < numSectors; i++ {
		start := i * a.sectorSize
		end := start + a.sectorSize
		if end > uint32(len(data)) {
			end = uint32(len(data))
		}

		sectorData := data[start:end]
		compressed, err := compressData(sectorData)
		if err != nil {
			return nil, 0, fmt.Errorf("compress sector %d: %w", i, err)
		}

		// Use compressed data if smaller
		if len(compressed) < len(sectorData) {
			sectors[i] = compressed
		} else {
			sectors[i] = sectorData
		}

		offsetTable[i] = currentOffset
		currentOffset += uint32(len(sectors[i]))

		// Calculate CRC for the uncompressed sector data
		if useCRC {
			sectorCRCs = append(sectorCRCs, adler32(sectorData))
		}
	}

	offsetTable[numSectors] = currentOffset

	// Build final data buffer
	totalSize := currentOffset
	result := make([]byte, totalSize)

	// Write offset table
	offset := uint32(0)
	for _, off := range offsetTable {
		result[offset] = byte(off)
		result[offset+1] = byte(off >> 8)
		result[offset+2] = byte(off >> 16)
		result[offset+3] = byte(off >> 24)
		offset += 4
	}

	// Write CRC table if needed
	if useCRC {
		for _, crc := range sectorCRCs {
			result[offset] = byte(crc)
			result[offset+1] = byte(crc >> 8)
			result[offset+2] = byte(crc >> 16)
			result[offset+3] = byte(crc >> 24)
			offset += 4
		}
	}

	// Write sector data
	for _, sector := range sectors {
		copy(result[offset:], sector)
		offset += uint32(len(sector))
	}

	return result, totalSize, nil
}

// addToHashTable adds a file to the hash table
func (a *Archive) addToHashTable(mpqPath string, blockIndex uint32) error {
	hashA := hashString(mpqPath, hashTypeNameA)
	hashB := hashString(mpqPath, hashTypeNameB)
	startIndex := hashString(mpqPath, hashTypeTableOffset) % a.header.HashTableSize

	for i := uint32(0); i < a.header.HashTableSize; i++ {
		idx := (startIndex + i) % a.header.HashTableSize
		entry := &a.hashTable[idx]

		if entry.BlockIndex == hashTableEmpty || entry.BlockIndex == hashTableDeleted {
			entry.HashA = hashA
			entry.HashB = hashB
			entry.Locale = localeNeutral
			entry.Platform = 0
			entry.BlockIndex = blockIndex
			return nil
		}
	}

	return fmt.Errorf("hash table full")
}
