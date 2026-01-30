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
	a.blockTable = make([]blockTableEntryEx, len(a.pendingFiles))
	listFileContent := ""
	needsHiBlockTable := false

	for i, pf := range a.pendingFiles {
		filePos, err := file.Seek(0, 1)
		if err != nil {
			return fmt.Errorf("get file position: %w", err)
		}

		if filePos > 0xFFFFFFFF {
			needsHiBlockTable = true
		}

		// Compress the data
		compressedData, err := compressData(pf.data)
		if err != nil {
			return fmt.Errorf("compress file %s: %w", pf.mpqPath, err)
		}

		// Use compressed data only if smaller
		var dataToWrite []byte
		var flags uint32 = fileExists | fileSingleUnit // Always use single-unit for simplicity

		if len(compressedData) < len(pf.data) {
			dataToWrite = compressedData
			flags |= fileCompress
		} else {
			dataToWrite = pf.data
		}

		if _, err := file.Write(dataToWrite); err != nil {
			return fmt.Errorf("write file data: %w", err)
		}

		// Add to block table
		a.blockTable[i] = blockTableEntryEx{
			blockTableEntry: blockTableEntry{
				FilePos:        uint32(filePos),
				CompressedSize: uint32(len(dataToWrite)),
				FileSize:       uint32(len(pf.data)),
				Flags:          flags,
			},
			FilePosHi: uint16(filePos >> 32),
		}

		// Add to hash table
		if err := a.addToHashTable(pf.mpqPath, uint32(i)); err != nil {
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

		blockIndex := uint32(len(a.blockTable))
		a.blockTable = append(a.blockTable, blockTableEntryEx{
			blockTableEntry: blockTableEntry{
				FilePos:        uint32(listFilePos),
				CompressedSize: uint32(len(dataToWrite)),
				FileSize:       uint32(len(listFileData)),
				Flags:          flags,
			},
			FilePosHi: uint16(listFilePos >> 32),
		})

		if err := a.addToHashTable("(listfile)", blockIndex); err != nil {
			return fmt.Errorf("add listfile to hash table: %w", err)
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

	// Get archive size
	archiveSize, _ := file.Seek(0, 1)

	// Update header
	a.header.setHashTableOffset64(uint64(hashTableOffset))
	a.header.setBlockTableOffset64(uint64(blockTableOffset))
	a.header.BlockTableSize = uint32(len(a.blockTable))
	a.header.ArchiveSize = uint32(archiveSize)

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
