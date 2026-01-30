// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FormatVersion specifies which MPQ format version to use when creating archives.
type FormatVersion int

const (
	// FormatV1 creates archives using the original MPQ format (up to 4GB).
	// Compatible with all games that use MPQ.
	FormatV1 FormatVersion = 0

	// FormatV2 creates archives using the extended format (>4GB support).
	// Compatible with WoW: The Burning Crusade and later.
	FormatV2 FormatVersion = 1
)

// Archive represents an MPQ archive.
type Archive struct {
	file          *os.File
	path          string
	tempPath      string
	mode          string // "r" for read, "w" for write
	header        *archiveHeader
	hashTable     []hashTableEntry
	blockTable    []blockTableEntryEx
	pendingFiles  []pendingFile
	sectorSize    uint32
	formatVersion FormatVersion
}

// pendingFile represents a file to be added to the archive.
type pendingFile struct {
	srcPath string
	mpqPath string
	data    []byte
}

// Create creates a new MPQ archive using V1 format.
// The maxFiles parameter specifies the maximum number of files the archive can hold.
func Create(path string, maxFiles int) (*Archive, error) {
	return CreateWithVersion(path, maxFiles, FormatV1)
}

// CreateV2 creates a new MPQ archive using V2 format.
// V2 format supports archives larger than 4GB and is compatible with
// WoW: The Burning Crusade and later.
func CreateV2(path string, maxFiles int) (*Archive, error) {
	return CreateWithVersion(path, maxFiles, FormatV2)
}

// CreateWithVersion creates a new MPQ archive with the specified format version.
func CreateWithVersion(path string, maxFiles int, version FormatVersion) (*Archive, error) {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}

	// Create temp file in same directory for atomic write
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, "mpq_*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()

	// Calculate hash table size (next power of 2 >= maxFiles * 1.5)
	hashTableSize := nextPowerOf2(uint32(float64(maxFiles) * 1.5))
	if hashTableSize < 16 {
		hashTableSize = 16
	}

	// Set header size based on version
	var headerSize uint32
	var formatVer uint16
	if version == FormatV2 {
		headerSize = headerSizeV2
		formatVer = formatVersion2
	} else {
		headerSize = headerSizeV1
		formatVer = formatVersion1
	}

	header := &archiveHeader{
		baseHeader: baseHeader{
			Magic:           mpqMagic,
			HeaderSize:      headerSize,
			FormatVersion:   formatVer,
			SectorSizeShift: defaultSectorSizeShift,
			HashTableSize:   hashTableSize,
			BlockTableSize:  0,
		},
	}

	return &Archive{
		path:          path,
		tempPath:      tempPath,
		mode:          "w",
		header:        header,
		hashTable:     make([]hashTableEntry, hashTableSize),
		blockTable:    make([]blockTableEntryEx, 0, maxFiles),
		pendingFiles:  make([]pendingFile, 0, maxFiles),
		sectorSize:    defaultSectorSize,
		formatVersion: version,
	}, nil
}

// Open opens an existing MPQ archive for reading.
// Supports both V1 and V2 format archives.
func Open(path string) (*Archive, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}

	// Read and validate header
	header, err := readArchiveHeader(file)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("read header: %w", err)
	}

	if header.Magic != mpqMagic {
		file.Close()
		return nil, fmt.Errorf("invalid MPQ magic: 0x%08X", header.Magic)
	}

	if header.FormatVersion > formatVersion2 {
		file.Close()
		return nil, fmt.Errorf("unsupported MPQ format version: %d (only V1 and V2 are supported)", header.FormatVersion)
	}

	// Read hash table
	hashTableOffset := header.getHashTableOffset64()
	if _, err := file.Seek(int64(hashTableOffset), io.SeekStart); err != nil {
		file.Close()
		return nil, fmt.Errorf("seek to hash table: %w", err)
	}

	hashTableData := make([]uint32, header.HashTableSize*4)
	if err := readUint32Array(file, hashTableData); err != nil {
		file.Close()
		return nil, fmt.Errorf("read hash table: %w", err)
	}
	decryptBlock(hashTableData, hashString("(hash table)", hashTypeFileKey))

	hashTable := make([]hashTableEntry, header.HashTableSize)
	for i := range hashTable {
		hashTable[i] = hashTableEntry{
			HashA:      hashTableData[i*4],
			HashB:      hashTableData[i*4+1],
			Locale:     uint16(hashTableData[i*4+2] & 0xFFFF),
			Platform:   uint16(hashTableData[i*4+2] >> 16),
			BlockIndex: hashTableData[i*4+3],
		}
	}

	// Read block table
	blockTableOffset := header.getBlockTableOffset64()
	if _, err := file.Seek(int64(blockTableOffset), io.SeekStart); err != nil {
		file.Close()
		return nil, fmt.Errorf("seek to block table: %w", err)
	}

	blockTableData := make([]uint32, header.BlockTableSize*4)
	if err := readUint32Array(file, blockTableData); err != nil {
		file.Close()
		return nil, fmt.Errorf("read block table: %w", err)
	}
	decryptBlock(blockTableData, hashString("(block table)", hashTypeFileKey))

	blockTable := make([]blockTableEntryEx, header.BlockTableSize)
	for i := range blockTable {
		blockTable[i] = blockTableEntryEx{
			blockTableEntry: blockTableEntry{
				FilePos:        blockTableData[i*4],
				CompressedSize: blockTableData[i*4+1],
				FileSize:       blockTableData[i*4+2],
				Flags:          blockTableData[i*4+3],
			},
			FilePosHi: 0,
		}
	}

	// Read extended block table if V2
	if header.FormatVersion >= formatVersion2 && header.HiBlockTableOffset64 != 0 {
		if _, err := file.Seek(int64(header.HiBlockTableOffset64), io.SeekStart); err != nil {
			file.Close()
			return nil, fmt.Errorf("seek to hi-block table: %w", err)
		}

		hiBlockTable := make([]uint16, header.BlockTableSize)
		if err := readUint16Array(file, hiBlockTable); err != nil {
			file.Close()
			return nil, fmt.Errorf("read hi-block table: %w", err)
		}

		for i := range blockTable {
			blockTable[i].FilePosHi = hiBlockTable[i]
		}
	}

	return &Archive{
		file:       file,
		path:       path,
		mode:       "r",
		header:     header,
		hashTable:  hashTable,
		blockTable: blockTable,
		sectorSize: 512 << header.SectorSizeShift,
	}, nil
}

// AddFile adds a file to the archive.
// The srcPath is the path to the file on disk.
// The mpqPath is the path within the archive (use backslashes or forward slashes).
// This method is only valid for archives opened with Create.
func (a *Archive) AddFile(srcPath, mpqPath string) error {
	if a.mode != "w" {
		return fmt.Errorf("archive not opened for writing")
	}

	// Normalize MPQ path
	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")

	// Read file data
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", srcPath, err)
	}

	a.pendingFiles = append(a.pendingFiles, pendingFile{
		srcPath: srcPath,
		mpqPath: mpqPath,
		data:    data,
	})

	return nil
}

// ExtractFile extracts a file from the archive to the specified destination.
// The mpqPath is the path within the archive (use backslashes or forward slashes).
// This method is only valid for archives opened with Open.
func (a *Archive) ExtractFile(mpqPath, destPath string) error {
	if a.mode != "r" {
		return fmt.Errorf("archive not opened for reading")
	}

	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")

	// Find file in hash table
	block, err := a.findFile(mpqPath)
	if err != nil {
		return err
	}

	// Read file data
	filePos := block.getFilePos64()
	if _, err := a.file.Seek(int64(filePos), io.SeekStart); err != nil {
		return fmt.Errorf("seek to file data: %w", err)
	}

	compressedData := make([]byte, block.CompressedSize)
	if _, err := io.ReadFull(a.file, compressedData); err != nil {
		return fmt.Errorf("read file data: %w", err)
	}

	var fileData []byte

	// Check if file is encrypted
	if block.Flags&fileEncrypted != 0 {
		// Compute encryption key from filename
		encryptionKey := getFileKey(mpqPath, filePos, block.FileSize, block.Flags)

		// Handle single-unit files vs sector-based files
		if block.Flags&fileSingleUnit != 0 {
			// Single unit file - decrypt as one block
			fileData, err = a.decryptAndDecompressSingleUnit(compressedData, block, encryptionKey)
			if err != nil {
				return fmt.Errorf("decrypt single unit file: %w", err)
			}
		} else {
			// Sector-based file - decrypt each sector
			fileData, err = a.decryptAndDecompressSectors(compressedData, block, encryptionKey)
			if err != nil {
				return fmt.Errorf("decrypt sectored file: %w", err)
			}
		}
	} else if block.Flags&fileCompress != 0 && block.CompressedSize < block.FileSize {
		// Not encrypted, but compressed
		if block.Flags&fileSingleUnit != 0 {
			// Single unit compressed file
			fileData, err = decompressData(compressedData, block.FileSize)
			if err != nil {
				return fmt.Errorf("decompress file: %w", err)
			}
		} else {
			// Sector-based compressed file
			fileData, err = a.decompressSectors(compressedData, block)
			if err != nil {
				return fmt.Errorf("decompress sectors: %w", err)
			}
		}
	} else {
		// Uncompressed, unencrypted
		fileData = compressedData
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	if err := os.WriteFile(destPath, fileData, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// decryptAndDecompressSingleUnit handles encrypted single-unit files
func (a *Archive) decryptAndDecompressSingleUnit(data []byte, block *blockTableEntryEx, key uint32) ([]byte, error) {
	// Decrypt the data
	decryptBytes(data, key)

	// Decompress if needed
	if block.Flags&fileCompress != 0 && block.CompressedSize < block.FileSize {
		return decompressData(data, block.FileSize)
	}

	return data, nil
}

// decryptAndDecompressSectors handles encrypted sector-based files
func (a *Archive) decryptAndDecompressSectors(data []byte, block *blockTableEntryEx, key uint32) ([]byte, error) {
	// Calculate number of sectors
	numSectors := (block.FileSize + a.sectorSize - 1) / a.sectorSize

	// Sector offset table is at the beginning of the data
	// It has numSectors+1 entries (last entry is end of last sector)
	offsetTableSize := (numSectors + 1) * 4

	if uint32(len(data)) < offsetTableSize {
		return nil, fmt.Errorf("data too small for sector offset table")
	}

	// Read and decrypt sector offset table
	offsetTable := make([]uint32, numSectors+1)
	for i := range offsetTable {
		offsetTable[i] = uint32(data[i*4]) |
			uint32(data[i*4+1])<<8 |
			uint32(data[i*4+2])<<16 |
			uint32(data[i*4+3])<<24
	}

	// Decrypt offset table with key-1
	decryptBlock(offsetTable, key-1)

	// Allocate output buffer
	result := make([]byte, 0, block.FileSize)

	// Process each sector
	for i := uint32(0); i < numSectors; i++ {
		sectorStart := offsetTable[i]
		sectorEnd := offsetTable[i+1]

		if sectorStart > uint32(len(data)) || sectorEnd > uint32(len(data)) || sectorEnd < sectorStart {
			return nil, fmt.Errorf("invalid sector offsets: %d-%d", sectorStart, sectorEnd)
		}

		sectorData := make([]byte, sectorEnd-sectorStart)
		copy(sectorData, data[sectorStart:sectorEnd])

		// Decrypt sector with key+sectorIndex
		decryptBytes(sectorData, key+i)

		// Calculate expected uncompressed size for this sector
		expectedSize := a.sectorSize
		if i == numSectors-1 {
			// Last sector may be smaller
			expectedSize = block.FileSize - (i * a.sectorSize)
		}

		// Decompress if needed
		if block.Flags&fileCompress != 0 && uint32(len(sectorData)) < expectedSize {
			decompressed, err := decompressData(sectorData, expectedSize)
			if err != nil {
				return nil, fmt.Errorf("decompress sector %d: %w", i, err)
			}
			result = append(result, decompressed...)
		} else {
			result = append(result, sectorData...)
		}
	}

	return result, nil
}

// decompressSectors handles unencrypted sector-based compressed files
func (a *Archive) decompressSectors(data []byte, block *blockTableEntryEx) ([]byte, error) {
	// Calculate number of sectors
	numSectors := (block.FileSize + a.sectorSize - 1) / a.sectorSize

	// Sector offset table is at the beginning of the data
	offsetTableSize := (numSectors + 1) * 4

	if uint32(len(data)) < offsetTableSize {
		return nil, fmt.Errorf("data too small for sector offset table")
	}

	// Read sector offset table (not encrypted)
	offsetTable := make([]uint32, numSectors+1)
	for i := range offsetTable {
		offsetTable[i] = uint32(data[i*4]) |
			uint32(data[i*4+1])<<8 |
			uint32(data[i*4+2])<<16 |
			uint32(data[i*4+3])<<24
	}

	// Allocate output buffer
	result := make([]byte, 0, block.FileSize)

	// Process each sector
	for i := uint32(0); i < numSectors; i++ {
		sectorStart := offsetTable[i]
		sectorEnd := offsetTable[i+1]

		if sectorStart > uint32(len(data)) || sectorEnd > uint32(len(data)) || sectorEnd < sectorStart {
			return nil, fmt.Errorf("invalid sector offsets: %d-%d", sectorStart, sectorEnd)
		}

		sectorData := data[sectorStart:sectorEnd]

		// Calculate expected uncompressed size for this sector
		expectedSize := a.sectorSize
		if i == numSectors-1 {
			expectedSize = block.FileSize - (i * a.sectorSize)
		}

		// Decompress if sector is smaller than expected
		if uint32(len(sectorData)) < expectedSize {
			decompressed, err := decompressData(sectorData, expectedSize)
			if err != nil {
				return nil, fmt.Errorf("decompress sector %d: %w", i, err)
			}
			result = append(result, decompressed...)
		} else {
			result = append(result, sectorData...)
		}
	}

	return result, nil
}

// HasFile returns true if the archive contains the specified file.
// The mpqPath is the path within the archive (use backslashes or forward slashes).
func (a *Archive) HasFile(mpqPath string) bool {
	if a.mode == "w" {
		mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")
		for _, f := range a.pendingFiles {
			if strings.EqualFold(f.mpqPath, mpqPath) {
				return true
			}
		}
		return false
	}

	_, err := a.findFile(mpqPath)
	return err == nil
}

// Close closes the archive.
// For archives opened with Create, this writes the archive to disk.
func (a *Archive) Close() error {
	if a.mode == "r" {
		if a.file != nil {
			return a.file.Close()
		}
		return nil
	}

	// Write mode
	if err := a.writeArchive(); err != nil {
		os.Remove(a.tempPath)
		return err
	}

	// Move temp file to final path
	os.Remove(a.path)
	if err := os.Rename(a.tempPath, a.path); err != nil {
		if err := copyFile(a.tempPath, a.path); err != nil {
			os.Remove(a.tempPath)
			return fmt.Errorf("save archive: %w", err)
		}
		os.Remove(a.tempPath)
	}

	return nil
}

// findFile looks up a file in the hash table and returns its block entry.
func (a *Archive) findFile(mpqPath string) (*blockTableEntryEx, error) {
	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")

	hashA := hashString(mpqPath, hashTypeNameA)
	hashB := hashString(mpqPath, hashTypeNameB)
	startIndex := hashString(mpqPath, hashTypeTableOffset) % a.header.HashTableSize

	for i := uint32(0); i < a.header.HashTableSize; i++ {
		idx := (startIndex + i) % a.header.HashTableSize
		entry := &a.hashTable[idx]

		if entry.BlockIndex == hashTableEmpty {
			break
		}
		if entry.BlockIndex == hashTableDeleted {
			continue
		}
		if entry.HashA == hashA && entry.HashB == hashB {
			if entry.BlockIndex < uint32(len(a.blockTable)) {
				block := &a.blockTable[entry.BlockIndex]
				if block.Flags&fileExists != 0 {
					return block, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("file not found: %s", mpqPath)
}

// nextPowerOf2 returns the smallest power of 2 >= n.
func nextPowerOf2(n uint32) uint32 {
	if n == 0 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	return n + 1
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
