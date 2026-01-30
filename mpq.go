// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

import (
	"encoding/binary"
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
	mode          string // "r" for read, "w" for write, "m" for modify
	header        *archiveHeader
	hashTable     []hashTableEntry
	blockTable    []blockTableEntryEx
	pendingFiles  []pendingFile
	removedFiles  map[string]bool // Files marked for removal in modify mode
	sectorSize    uint32
	formatVersion FormatVersion
}

// pendingFile represents a file to be added to the archive.
type pendingFile struct {
	srcPath        string
	mpqPath        string
	data           []byte
	generateCRC    bool // Whether to generate sector CRC for this file
	isPatchFile    bool // Mark as a patch file (FILE_PATCH_FILE)
	isDeleteMarker bool // Mark as a deletion marker (FILE_DELETE_MARKER)
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
		removedFiles:  make(map[string]bool),
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

	// Read and validate header (scan for embedded headers)
	header, err := findArchiveHeader(file)
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
	hashTableOffset := header.getHashTableOffset64() + header.ArchiveOffset
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
	blockTableOffset := header.getBlockTableOffset64() + header.ArchiveOffset
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
		hiBlockOffset := header.HiBlockTableOffset64 + header.ArchiveOffset
		if _, err := file.Seek(int64(hiBlockOffset), io.SeekStart); err != nil {
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
		sectorSize: 1 << header.SectorSizeShift,
	}, nil
}

// OpenForModify opens an existing MPQ archive for modification.
// This allows adding, removing, and replacing files in an existing archive.
// The archive is re-written when Close() is called.
func OpenForModify(path string) (*Archive, error) {
	// First open the archive for reading to load its contents
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}

	// Read and validate header
	header, err := findArchiveHeader(file)
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
	hashTableOffset := header.getHashTableOffset64() + header.ArchiveOffset
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
	blockTableOffset := header.getBlockTableOffset64() + header.ArchiveOffset
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
		hiBlockOffset := header.HiBlockTableOffset64 + header.ArchiveOffset
		if _, err := file.Seek(int64(hiBlockOffset), io.SeekStart); err != nil {
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

	// Determine format version from header
	var formatVer FormatVersion
	if header.FormatVersion >= formatVersion2 {
		formatVer = FormatV2
	} else {
		formatVer = FormatV1
	}

	// Create temp file for modifications
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, "mpq_*.tmp")
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()

	return &Archive{
		file:          file,
		path:          path,
		tempPath:      tempPath,
		mode:          "m", // modify mode
		header:        header,
		hashTable:     hashTable,
		blockTable:    blockTable,
		pendingFiles:  make([]pendingFile, 0),
		removedFiles:  make(map[string]bool),
		sectorSize:    1 << header.SectorSizeShift,
		formatVersion: formatVer,
	}, nil
}

// AddFile adds a file to the archive.
// The srcPath is the path to the file on disk.
// The mpqPath is the path within the archive (use backslashes or forward slashes).
// This method is only valid for archives opened with Create.
func (a *Archive) AddFile(srcPath, mpqPath string) error {
	return a.AddFileWithOptions(srcPath, mpqPath, false)
}

// AddFileWithCRC adds a file to the archive with sector CRC generation enabled.
// The srcPath is the path to the file on disk.
// The mpqPath is the path within the archive (use backslashes or forward slashes).
// This method is only valid for archives opened with Create.
func (a *Archive) AddFileWithCRC(srcPath, mpqPath string) error {
	return a.AddFileWithOptions(srcPath, mpqPath, true)
}

// AddFileWithOptions adds a file to the archive with specified options.
func (a *Archive) AddFileWithOptions(srcPath, mpqPath string, generateCRC bool) error {
	if a.mode != "w" && a.mode != "m" {
		return fmt.Errorf("archive not opened for writing or modification")
	}

	// Normalize MPQ path
	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")

	// Read file data
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", srcPath, err)
	}

	a.pendingFiles = append(a.pendingFiles, pendingFile{
		srcPath:     srcPath,
		mpqPath:     mpqPath,
		data:        data,
		generateCRC: generateCRC,
	})

	return nil
}

// AddPatchFile adds a file marked as a patch file (FILE_PATCH_FILE).
// Patch files are typically used in MPQ patch archives.
func (a *Archive) AddPatchFile(srcPath, mpqPath string) error {
	if a.mode != "w" && a.mode != "m" {
		return fmt.Errorf("archive not opened for writing or modification")
	}

	// Normalize MPQ path
	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")

	// Read file data
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", srcPath, err)
	}

	a.pendingFiles = append(a.pendingFiles, pendingFile{
		srcPath:     srcPath,
		mpqPath:     mpqPath,
		data:        data,
		isPatchFile: true,
	})

	return nil
}

// AddDeleteMarker adds a deletion marker for a file.
// This is used in patch archives to indicate that a file should be deleted.
func (a *Archive) AddDeleteMarker(mpqPath string) error {
	if a.mode != "w" && a.mode != "m" {
		return fmt.Errorf("archive not opened for writing or modification")
	}

	// Normalize MPQ path
	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")

	a.pendingFiles = append(a.pendingFiles, pendingFile{
		mpqPath:        mpqPath,
		data:           nil, // No data for deletion markers
		isDeleteMarker: true,
	})

	return nil
}

// RemoveFile marks a file for removal from the archive.
// This is only valid for archives opened with OpenForModify.
// The file will be excluded when the archive is re-written on Close().
func (a *Archive) RemoveFile(mpqPath string) error {
	if a.mode != "m" {
		return fmt.Errorf("archive not opened for modification")
	}

	// Normalize MPQ path
	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")

	// Check if file exists in archive
	if !a.HasFile(mpqPath) {
		return fmt.Errorf("file not found in archive: %s", mpqPath)
	}

	// Mark file as removed
	a.removedFiles[mpqPath] = true
	return nil
}

// ExtractFile extracts a file from the archive to the specified destination.
// The mpqPath is the path within the archive (use backslashes or forward slashes).
// This method is valid for archives opened with Open or OpenForModify.
func (a *Archive) ExtractFile(mpqPath, destPath string) error {
	if a.mode != "r" && a.mode != "m" {
		return fmt.Errorf("archive not opened for reading")
	}

	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")

	// Find file in hash table
	block, err := a.findFile(mpqPath)
	if err != nil {
		return err
	}

	// Read file data
	blockPos := block.getFilePos64()
	filePos := blockPos + a.header.ArchiveOffset
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
		encryptionKey := getFileKey(mpqPath, blockPos, block.FileSize, block.Flags)

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
	} else if block.Flags&fileCompress != 0 {
		// Compressed file (single unit or sectors)
		if block.Flags&fileSingleUnit != 0 {
			// Single unit compressed file
			dataToDecompress := compressedData
			
			// Handle sector CRC for single unit files
			if block.Flags&fileSectorCRC != 0 {
				if len(compressedData) < 4 {
					return fmt.Errorf("missing sector CRC for single unit file")
				}
				dataToDecompress = compressedData[:len(compressedData)-4]
				crcExpected := binary.LittleEndian.Uint32(compressedData[len(compressedData)-4:])
				
				// Decompress first, then validate CRC
				decompressed, err := decompressData(dataToDecompress, block.FileSize)
				if err != nil {
					return fmt.Errorf("decompress file: %w", err)
				}
				
				crcActual := adler32(decompressed)
				if crcActual != crcExpected {
					return fmt.Errorf("sector CRC mismatch: expected 0x%08X got 0x%08X", crcExpected, crcActual)
				}
				fileData = decompressed
			} else {
				// Only decompress if compressed size is smaller
				if block.CompressedSize < block.FileSize {
					fileData, err = decompressData(dataToDecompress, block.FileSize)
					if err != nil {
						return fmt.Errorf("decompress file: %w", err)
					}
				} else {
					fileData = dataToDecompress
				}
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
		// Handle sector CRC for uncompressed single unit files
		if block.Flags&fileSingleUnit != 0 && block.Flags&fileSectorCRC != 0 {
			if len(compressedData) < 4 {
				return fmt.Errorf("missing sector CRC for single unit file")
			}
			payload := compressedData[:len(compressedData)-4]
			crcExpected := binary.LittleEndian.Uint32(compressedData[len(compressedData)-4:])
			crcActual := adler32(payload)
			if crcActual != crcExpected {
				return fmt.Errorf("sector CRC mismatch: expected 0x%08X got 0x%08X", crcExpected, crcActual)
			}
			fileData = payload
		} else {
			fileData = compressedData
		}
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

	// Validate CRC if present for single-unit files
	if block.Flags&fileSectorCRC != 0 {
		if len(data) < 4 {
			return nil, fmt.Errorf("missing sector CRC for single unit file")
		}
		payload := data[:len(data)-4]
		crcExpected := binary.LittleEndian.Uint32(data[len(data)-4:])
		crcActual := adler32(payload)
		if crcActual != crcExpected {
			return nil, fmt.Errorf("sector CRC mismatch: expected 0x%08X got 0x%08X", crcExpected, crcActual)
		}
		return payload, nil
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

	dataOffset := uint32(offsetTableSize)
	var sectorCRCs []uint32
	if block.Flags&fileSectorCRC != 0 && len(offsetTable) > 0 {
		firstDataOffset := offsetTable[0]
		crcTableSize := numSectors * 4
		crcTableEnd := uint32(offsetTableSize) + crcTableSize
		if firstDataOffset >= crcTableEnd {
			if int(crcTableEnd) > len(data) {
				return nil, fmt.Errorf("sector CRC table out of range")
			}
			sectorCRCs = make([]uint32, numSectors)
			for i := uint32(0); i < numSectors; i++ {
				start := offsetTableSize + i*4
				sectorCRCs[i] = binary.LittleEndian.Uint32(data[start : start+4])
			}
			decryptBlock(sectorCRCs, key-1+numSectors)
			dataOffset = crcTableEnd
		}
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
		var sectorOutput []byte
		if block.Flags&fileCompress != 0 && uint32(len(sectorData)) < expectedSize {
			decompressed, err := decompressData(sectorData, expectedSize)
			if err != nil {
				return nil, fmt.Errorf("decompress sector %d: %w", i, err)
			}
			sectorOutput = decompressed
		} else {
			sectorOutput = sectorData
		}

		if len(sectorCRCs) > 0 {
			crcActual := adler32(sectorOutput)
			crcExpected := sectorCRCs[i]
			if crcActual != crcExpected {
				return nil, fmt.Errorf("sector CRC mismatch for sector %d: expected 0x%08X got 0x%08X", i, crcExpected, crcActual)
			}
		}

		result = append(result, sectorOutput...)
	}

	_ = dataOffset
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
			return nil, fmt.Errorf("invalid sector offsets: %d-%d (data len %d)", sectorStart, sectorEnd, len(data))
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

// ListFiles returns a list of files in the archive by reading the (listfile).
func (a *Archive) ListFiles() ([]string, error) {
	if a.mode != "r" && a.mode != "m" {
		return nil, fmt.Errorf("archive not opened for reading")
	}

	// Try to extract the listfile to a temp file
	tmpFile, err := os.CreateTemp("", "mpq_listfile_*")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	if err := a.ExtractFile("(listfile)", tmpPath); err != nil {
		return nil, fmt.Errorf("extract listfile: %w", err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("read listfile: %w", err)
	}

	// Parse listfile (one file per line, may have \r\n or \n)
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(content, "\n")

	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && line != "(listfile)" {
			files = append(files, line)
		}
	}

	return files, nil
}

// HasFile returns true if the archive contains the specified file.
// The mpqPath is the path within the archive (use backslashes or forward slashes).
// Files marked as deletion markers return false.
func (a *Archive) HasFile(mpqPath string) bool {
	if a.mode == "w" {
		mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")
		for _, f := range a.pendingFiles {
			if strings.EqualFold(f.mpqPath, mpqPath) {
				return !f.isDeleteMarker
			}
		}
		return false
	}

	block, err := a.findFile(mpqPath)
	if err != nil {
		return false
	}
	// Check for deletion marker
	return block.Flags&fileDeleteMarker == 0
}

// IsDeleteMarker returns true if the file is marked for deletion (used in patches).
func (a *Archive) IsDeleteMarker(mpqPath string) bool {
	if a.mode != "r" {
		return false
	}

	block, err := a.findFile(mpqPath)
	if err != nil {
		return false
	}

	return block.Flags&fileDeleteMarker != 0
}

// IsPatchFile returns true if the file is marked as a patch file.
func (a *Archive) IsPatchFile(mpqPath string) bool {
	if a.mode != "r" {
		return false
	}

	block, err := a.findFile(mpqPath)
	if err != nil {
		return false
	}

	return block.Flags&filePatchFile != 0
}

// Close closes the archive.
// For archives opened with Create or OpenForModify, this writes the archive to disk.
func (a *Archive) Close() error {
	if a.mode == "r" {
		if a.file != nil {
			return a.file.Close()
		}
		return nil
	}

	// Write or modify mode - need to write the archive
	if a.mode == "m" {
		// Modify mode: build pending files from existing archive, excluding removed files
		if err := a.buildModifiedFileList(); err != nil {
			if a.file != nil {
				a.file.Close()
			}
			os.Remove(a.tempPath)
			return err
		}
		// Close the source file before writing
		if a.file != nil {
			a.file.Close()
			a.file = nil
		}
	}

	// Write the archive (works for both "w" and "m" modes)
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

// buildModifiedFileList constructs the pending file list for modify mode.
// It includes all existing files (not removed) plus any new/replaced files from pendingFiles.
func (a *Archive) buildModifiedFileList() error {
	// Get list of all files in the archive
	fileList, err := a.ListFiles()
	if err != nil {
		return fmt.Errorf("list files: %w", err)
	}

	// Build a map of pending files for quick lookup
	pendingMap := make(map[string]pendingFile)
	for _, pf := range a.pendingFiles {
		normalizedPath := strings.ReplaceAll(pf.mpqPath, "/", "\\")
		pendingMap[normalizedPath] = pf
	}

	// Build new pending files list combining existing + new/replaced files
	newPendingFiles := make([]pendingFile, 0)

	// Process existing files
	for _, mpqPath := range fileList {
		normalizedPath := strings.ReplaceAll(mpqPath, "/", "\\")

		// Skip removed files
		if a.removedFiles[normalizedPath] {
			continue
		}

		// Skip special files - they'll be regenerated
		if normalizedPath == "(listfile)" || normalizedPath == "(attributes)" {
			continue
		}

		// Check if this file is being replaced
		if pending, exists := pendingMap[normalizedPath]; exists {
			// Use the new version
			newPendingFiles = append(newPendingFiles, pending)
			delete(pendingMap, normalizedPath) // Mark as processed
		} else {
			// Keep the existing file - extract its data
			block, err := a.findFile(normalizedPath)
			if err != nil {
				continue // Skip files we can't find
			}

			// Read the file data from the archive
			if _, err := a.file.Seek(int64(block.getFilePos64()+a.header.ArchiveOffset), io.SeekStart); err != nil {
				return fmt.Errorf("seek to file %s: %w", normalizedPath, err)
			}

			fileData := make([]byte, block.CompressedSize)
			if _, err := io.ReadFull(a.file, fileData); err != nil {
				return fmt.Errorf("read file %s: %w", normalizedPath, err)
			}

			// Determine if file has CRC
			hasCRC := block.Flags&fileSectorCRC != 0

			// Check if it's a patch file or deletion marker
			isPatch := block.Flags&filePatchFile != 0
			isDelete := block.Flags&fileDeleteMarker != 0

			// For modify mode, we need to extract and re-add the file
			// Extract the actual file content (decompress if needed)
			var extractedData []byte
			if block.Flags&fileExists == 0 || isDelete {
				// Deletion marker - preserve it
				newPendingFiles = append(newPendingFiles, pendingFile{
					mpqPath:        normalizedPath,
					data:           nil,
					isDeleteMarker: true,
				})
				continue
			}

			// Decrypt if needed
			if block.Flags&fileEncrypted != 0 {
				key := hashString(filepath.Base(normalizedPath), hashTypeFileKey)
				if block.Flags&fileFixKey != 0 {
					key = (key + block.FilePos) ^ block.FileSize
				}

				if block.Flags&fileSingleUnit != 0 {
					extractedData, err = a.decryptAndDecompressSingleUnit(fileData, block, key)
				} else {
					extractedData, err = a.decryptAndDecompressSectors(fileData, block, key)
				}
				if err != nil {
					return fmt.Errorf("decrypt file %s: %w", normalizedPath, err)
				}
			} else if block.Flags&fileCompress != 0 {
				// Compressed but not encrypted
				if block.Flags&fileSingleUnit != 0 {
					// Single-unit compressed file
					dataToDecompress := fileData
					if block.Flags&fileSectorCRC != 0 {
						// Strip CRC from end
						if len(dataToDecompress) < 4 {
							return fmt.Errorf("file %s too short for CRC", normalizedPath)
						}
						dataToDecompress = dataToDecompress[:len(dataToDecompress)-4]
					}
					if block.CompressedSize < block.FileSize {
						extractedData, err = decompressData(dataToDecompress, block.FileSize)
						if err != nil {
							return fmt.Errorf("decompress file %s: %w", normalizedPath, err)
						}
					} else {
						extractedData = dataToDecompress
					}
				} else {
					// Multi-sector compressed file
					extractedData, err = a.decompressSectors(fileData, block)
					if err != nil {
						return fmt.Errorf("decompress sectors %s: %w", normalizedPath, err)
					}
				}
			} else {
				// Uncompressed, unencrypted
				if block.Flags&fileSingleUnit != 0 && block.Flags&fileSectorCRC != 0 {
					// Strip CRC from end
					if len(fileData) < 4 {
						return fmt.Errorf("file %s too short for CRC", normalizedPath)
					}
					extractedData = fileData[:len(fileData)-4]
				} else {
					extractedData = fileData
				}
			}

			newPendingFiles = append(newPendingFiles, pendingFile{
				mpqPath:     normalizedPath,
				data:        extractedData,
				generateCRC: hasCRC,
				isPatchFile: isPatch,
			})
		}
	}

	// Add any new files that weren't in the original archive
	for _, pending := range pendingMap {
		newPendingFiles = append(newPendingFiles, pending)
	}

	// Replace the pending files list
	a.pendingFiles = newPendingFiles

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

// readPatchMetadata reads the (patch_metadata) special file if present.
// Returns nil if the file doesn't exist or can't be parsed.
func (a *Archive) readPatchMetadata() (*PatchMetadata, error) {
	if a.mode != "r" {
		return nil, fmt.Errorf("archive not opened for reading")
	}

	// Check if patch_metadata file exists
	block, err := a.findFile("(patch_metadata)")
	if err != nil {
		return nil, nil // Patch metadata is optional
	}

	// Read patch_metadata file data
	blockPos := block.getFilePos64()
	filePos := blockPos + a.header.ArchiveOffset
	if _, err := a.file.Seek(int64(filePos), 0); err != nil {
		return nil, fmt.Errorf("seek to patch_metadata: %w", err)
	}

	compressedData := make([]byte, block.CompressedSize)
	if n, err := a.file.Read(compressedData); err != nil || n != int(block.CompressedSize) {
		return nil, fmt.Errorf("read patch_metadata: %w", err)
	}

	var metadataBytes []byte

	// Decompress if needed
	if block.Flags&fileCompress != 0 && block.CompressedSize < block.FileSize {
		decompressed, err := decompressData(compressedData, block.FileSize)
		if err != nil {
			return nil, fmt.Errorf("decompress patch_metadata: %w", err)
		}
		metadataBytes = decompressed
	} else {
		metadataBytes = compressedData
	}

	if len(metadataBytes) < 36 {
		return nil, fmt.Errorf("patch_metadata too small: %d bytes", len(metadataBytes))
	}

	meta := &PatchMetadata{}
	copy(meta.BaseMD5[:], metadataBytes[0:16])
	copy(meta.PatchMD5[:], metadataBytes[16:32])
	meta.BaseFileSize = uint32(metadataBytes[32]) |
		uint32(metadataBytes[33])<<8 |
		uint32(metadataBytes[34])<<16 |
		uint32(metadataBytes[35])<<24

	return meta, nil
}

// PatchMetadata contains information about a patch file.
type PatchMetadata struct {
	BaseMD5      [16]byte // MD5 of the base file this patch applies to
	PatchMD5     [16]byte // MD5 of the patch file itself
	BaseFileSize uint32   // Size of base file
}
