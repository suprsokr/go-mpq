// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAndRead(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	testFile1 := filepath.Join(tmpDir, "test1.txt")
	testFile2 := filepath.Join(tmpDir, "test2.txt")
	testContent1 := []byte("Hello, World! This is test file 1 with some content.")
	testContent2 := []byte("Test file 2 contains different data for the archive.")

	if err := os.WriteFile(testFile1, testContent1, 0644); err != nil {
		t.Fatalf("write test file 1: %v", err)
	}
	if err := os.WriteFile(testFile2, testContent2, 0644); err != nil {
		t.Fatalf("write test file 2: %v", err)
	}

	// Create archive
	mpqPath := filepath.Join(tmpDir, "test.mpq")
	archive, err := Create(mpqPath, 10)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}

	if err := archive.AddFile(testFile1, "Data\\Test1.txt"); err != nil {
		t.Fatalf("add file 1: %v", err)
	}
	if err := archive.AddFile(testFile2, "Data\\SubDir\\Test2.txt"); err != nil {
		t.Fatalf("add file 2: %v", err)
	}

	if err := archive.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(mpqPath); os.IsNotExist(err) {
		t.Fatalf("MPQ file not created")
	}

	// Open and read
	readArchive, err := Open(mpqPath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer readArchive.Close()

	// Check files exist
	if !readArchive.HasFile("Data\\Test1.txt") {
		t.Errorf("file 1 not found")
	}
	if !readArchive.HasFile("Data\\SubDir\\Test2.txt") {
		t.Errorf("file 2 not found")
	}
	if readArchive.HasFile("NonExistent.txt") {
		t.Errorf("non-existent file found")
	}

	// Extract and verify
	extractDir := filepath.Join(tmpDir, "extracted")
	extract1 := filepath.Join(extractDir, "test1.txt")
	extract2 := filepath.Join(extractDir, "test2.txt")

	if err := readArchive.ExtractFile("Data\\Test1.txt", extract1); err != nil {
		t.Fatalf("extract file 1: %v", err)
	}
	if err := readArchive.ExtractFile("Data\\SubDir\\Test2.txt", extract2); err != nil {
		t.Fatalf("extract file 2: %v", err)
	}

	extracted1, _ := os.ReadFile(extract1)
	if string(extracted1) != string(testContent1) {
		t.Errorf("file 1 mismatch: got %q, want %q", extracted1, testContent1)
	}

	extracted2, _ := os.ReadFile(extract2)
	if string(extracted2) != string(testContent2) {
		t.Errorf("file 2 mismatch: got %q, want %q", extracted2, testContent2)
	}
}

func TestHashString(t *testing.T) {
	// Test cases based on StormLib's known hash values
	// These are the decryption keys defined in StormLib.h:
	// MPQ_KEY_HASH_TABLE = 0xC3AF3770 (HashString("(hash table)", MPQ_HASH_FILE_KEY))
	// MPQ_KEY_BLOCK_TABLE = 0xEC83B3A3 (HashString("(block table)", MPQ_HASH_FILE_KEY))
	tests := []struct {
		input    string
		hashType uint32
		expected uint32
	}{
		// Key derivation tests (from StormLib.h constants)
		{"(hash table)", hashTypeFileKey, 0xC3AF3770},
		{"(block table)", hashTypeFileKey, 0xEC83B3A3},
	}

	for _, test := range tests {
		got := hashString(test.input, test.hashType)
		if got != test.expected {
			t.Errorf("hashString(%q, %d) = 0x%08X, want 0x%08X",
				test.input, test.hashType, got, test.expected)
		}
	}
}

// TestHashStringFromStormLib tests hash values that can be derived from StormLib test data
// These test the HashA (hashTypeNameA) and HashB (hashTypeNameB) functions used for file lookups
func TestHashStringFromStormLib(t *testing.T) {
	// From StormLib's StormTest.cpp HashVals test data:
	// {0x8bd6929a, 0xfd55129b, "ReplaceableTextures\\CommandButtons\\BTNHaboss79.blp"}
	// dwHash1 = HashA, dwHash2 = HashB
	tests := []struct {
		name     string
		input    string
		hashA    uint32 // hashTypeNameA (1)
		hashB    uint32 // hashTypeNameB (2)
	}{
		{
			name:  "StormLib test file path",
			input: "ReplaceableTextures\\CommandButtons\\BTNHaboss79.blp",
			hashA: 0x8bd6929a,
			hashB: 0xfd55129b,
		},
		{
			name:  "StormLib test file path with forward slashes",
			input: "ReplaceableTextures/CommandButtons/BTNHaboss79.blp",
			hashA: 0x8bd6929a, // Should be same - slashes are normalized
			hashB: 0xfd55129b,
		},
		{
			name:  "StormLib test file path lowercase",
			input: "replaceabletextures\\commandbuttons\\btnhaboss79.blp",
			hashA: 0x8bd6929a, // Should be same - case insensitive
			hashB: 0xfd55129b,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotA := hashString(test.input, hashTypeNameA)
			gotB := hashString(test.input, hashTypeNameB)

			if gotA != test.hashA {
				t.Errorf("hashString(%q, hashTypeNameA) = 0x%08X, want 0x%08X",
					test.input, gotA, test.hashA)
			}
			if gotB != test.hashB {
				t.Errorf("hashString(%q, hashTypeNameB) = 0x%08X, want 0x%08X",
					test.input, gotB, test.hashB)
			}
		})
	}
}

// TestCryptTableInitialization verifies the crypt table is initialized correctly
// by checking known values that can be derived from the StormLib algorithm
func TestCryptTableInitialization(t *testing.T) {
	// The crypt table should be 0x500 (1280) entries
	if len(cryptTable) != 0x500 {
		t.Errorf("cryptTable length = %d, want %d", len(cryptTable), 0x500)
	}

	// Verify some specific values by re-computing them
	// The algorithm is deterministic, so we can verify the initialization
	seed := uint32(0x00100001)
	for index1 := 0; index1 < 0x100; index1++ {
		index2 := index1
		for i := 0; i < 5; i++ {
			seed = (seed*125 + 3) % 0x2AAAAB
			temp1 := (seed & 0xFFFF) << 0x10
			seed = (seed*125 + 3) % 0x2AAAAB
			temp2 := seed & 0xFFFF
			expected := temp1 | temp2

			if cryptTable[index2] != expected {
				t.Errorf("cryptTable[0x%03X] = 0x%08X, want 0x%08X", index2, cryptTable[index2], expected)
			}
			index2 += 0x100
		}
	}
}

func TestPathNormalization(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_path_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	mpqPath := filepath.Join(tmpDir, "test.mpq")
	archive, err := Create(mpqPath, 10)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}

	// Add with forward slashes
	if err := archive.AddFile(testFile, "Interface/AddOns/Test.lua"); err != nil {
		t.Fatalf("add file: %v", err)
	}
	archive.Close()

	// Read and verify both slash styles work
	readArchive, _ := Open(mpqPath)
	defer readArchive.Close()

	if !readArchive.HasFile("Interface\\AddOns\\Test.lua") {
		t.Errorf("file not found with backslashes")
	}
	if !readArchive.HasFile("Interface/AddOns/Test.lua") {
		t.Errorf("file not found with forward slashes")
	}
}

func TestV2Format(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_v2_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := []byte("V2 format test content")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	// Create V2 archive
	mpqPath := filepath.Join(tmpDir, "test_v2.mpq")
	archive, err := CreateV2(mpqPath, 10)
	if err != nil {
		t.Fatalf("create V2 archive: %v", err)
	}

	if err := archive.AddFile(testFile, "Data\\Test.txt"); err != nil {
		t.Fatalf("add file: %v", err)
	}
	archive.Close()

	// Read back
	readArchive, err := Open(mpqPath)
	if err != nil {
		t.Fatalf("open V2 archive: %v", err)
	}
	defer readArchive.Close()

	if !readArchive.HasFile("Data\\Test.txt") {
		t.Errorf("file not found in V2 archive")
	}

	extractPath := filepath.Join(tmpDir, "extracted.txt")
	if err := readArchive.ExtractFile("Data\\Test.txt", extractPath); err != nil {
		t.Fatalf("extract file: %v", err)
	}

	extracted, _ := os.ReadFile(extractPath)
	if string(extracted) != string(testContent) {
		t.Errorf("content mismatch: got %q, want %q", extracted, testContent)
	}
}

func TestV1V2HeaderSizes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_header_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	// V1
	v1Path := filepath.Join(tmpDir, "v1.mpq")
	v1, _ := Create(v1Path, 10)
	v1.AddFile(testFile, "test.txt")
	v1.Close()

	// V2
	v2Path := filepath.Join(tmpDir, "v2.mpq")
	v2, _ := CreateV2(v2Path, 10)
	v2.AddFile(testFile, "test.txt")
	v2.Close()

	// Check header sizes
	v1File, _ := os.Open(v1Path)
	defer v1File.Close()
	v1Header := make([]byte, 8)
	v1File.Read(v1Header)
	v1Size := uint32(v1Header[4]) | uint32(v1Header[5])<<8 | uint32(v1Header[6])<<16 | uint32(v1Header[7])<<24
	if v1Size != 0x20 {
		t.Errorf("V1 header size: got 0x%X, want 0x20", v1Size)
	}

	v2File, _ := os.Open(v2Path)
	defer v2File.Close()
	v2Header := make([]byte, 8)
	v2File.Read(v2Header)
	v2Size := uint32(v2Header[4]) | uint32(v2Header[5])<<8 | uint32(v2Header[6])<<16 | uint32(v2Header[7])<<24
	if v2Size != 0x2C {
		t.Errorf("V2 header size: got 0x%X, want 0x2C", v2Size)
	}
}

func TestEmptyArchive(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_empty_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mpqPath := filepath.Join(tmpDir, "empty.mpq")
	archive, err := Create(mpqPath, 10)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	archive.Close()

	if _, err := os.Stat(mpqPath); os.IsNotExist(err) {
		t.Fatalf("MPQ file not created")
	}

	readArchive, err := Open(mpqPath)
	if err != nil {
		t.Fatalf("open empty archive: %v", err)
	}
	defer readArchive.Close()

	if readArchive.HasFile("anything.txt") {
		t.Errorf("found file in empty archive")
	}
}

func TestLargeFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_large_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create 100KB file
	testFile := filepath.Join(tmpDir, "large.bin")
	largeData := make([]byte, 100*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}
	os.WriteFile(testFile, largeData, 0644)

	mpqPath := filepath.Join(tmpDir, "large.mpq")
	archive, _ := Create(mpqPath, 10)
	archive.AddFile(testFile, "Data\\Large.bin")
	archive.Close()

	// Verify compression occurred
	mpqInfo, _ := os.Stat(mpqPath)
	if mpqInfo.Size() >= int64(len(largeData)) {
		t.Logf("Note: compressed size (%d) >= original (%d)", mpqInfo.Size(), len(largeData))
	}

	// Extract and verify
	readArchive, _ := Open(mpqPath)
	defer readArchive.Close()

	extractPath := filepath.Join(tmpDir, "extracted.bin")
	readArchive.ExtractFile("Data\\Large.bin", extractPath)

	extracted, _ := os.ReadFile(extractPath)
	if len(extracted) != len(largeData) {
		t.Fatalf("size mismatch: got %d, want %d", len(extracted), len(largeData))
	}

	for i := range largeData {
		if extracted[i] != largeData[i] {
			t.Fatalf("data mismatch at byte %d", i)
		}
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	// Test data for encrypt/decrypt round-trip
	testCases := []struct {
		name string
		data []uint32
		key  string
	}{
		{
			name: "hash table key",
			data: []uint32{0x12345678, 0xDEADBEEF, 0xCAFEBABE, 0xF00DF00D},
			key:  "(hash table)",
		},
		{
			name: "block table key",
			data: []uint32{0x11111111, 0x22222222, 0x33333333, 0x44444444},
			key:  "(block table)",
		},
		{
			name: "single value",
			data: []uint32{0xABCDEF01},
			key:  "(hash table)",
		},
		{
			name: "zeros",
			data: []uint32{0x00000000, 0x00000000, 0x00000000, 0x00000000},
			key:  "(hash table)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Make a copy of original data
			original := make([]uint32, len(tc.data))
			copy(original, tc.data)

			// Working copy for encryption/decryption
			data := make([]uint32, len(tc.data))
			copy(data, tc.data)

			// Get the encryption key
			key := hashString(tc.key, hashTypeFileKey)

			// Encrypt
			encryptBlock(data, key)

			// Verify data changed (except for edge cases like all zeros with specific keys)
			allSame := true
			for i := range data {
				if data[i] != original[i] {
					allSame = false
					break
				}
			}
			if allSame && tc.name != "zeros" {
				t.Errorf("encryption did not change data")
			}

			// Decrypt
			decryptBlock(data, key)

			// Verify round-trip
			for i := range original {
				if data[i] != original[i] {
					t.Errorf("round-trip mismatch at index %d: got 0x%08X, want 0x%08X",
						i, data[i], original[i])
				}
			}
		})
	}
}

func TestSectorCRCGeneration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_crc_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := []byte("Test content for sector CRC validation")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	// Create archive with CRC
	mpqPath := filepath.Join(tmpDir, "test_crc.mpq")
	archive, err := Create(mpqPath, 10)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}

	if err := archive.AddFileWithCRC(testFile, "Data\\Test.txt"); err != nil {
		t.Fatalf("add file with CRC: %v", err)
	}
	archive.Close()

	// Read back and verify
	readArchive, err := Open(mpqPath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer readArchive.Close()

	extractPath := filepath.Join(tmpDir, "extracted.txt")
	if err := readArchive.ExtractFile("Data\\Test.txt", extractPath); err != nil {
		t.Fatalf("extract file with CRC: %v", err)
	}

	extracted, _ := os.ReadFile(extractPath)
	if string(extracted) != string(testContent) {
		t.Errorf("content mismatch: got %q, want %q", extracted, testContent)
	}
}

func TestPatchFileMarker(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_patch_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create patch file
	patchFile := filepath.Join(tmpDir, "patch.dat")
	patchContent := []byte("Patch data")
	if err := os.WriteFile(patchFile, patchContent, 0644); err != nil {
		t.Fatalf("write patch file: %v", err)
	}

	// Create archive with patch file
	mpqPath := filepath.Join(tmpDir, "patch.mpq")
	archive, err := Create(mpqPath, 10)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}

	if err := archive.AddPatchFile(patchFile, "Data\\Patch.dat"); err != nil {
		t.Fatalf("add patch file: %v", err)
	}
	archive.Close()

	// Read back and verify patch flag
	readArchive, err := Open(mpqPath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer readArchive.Close()

	if !readArchive.IsPatchFile("Data\\Patch.dat") {
		t.Errorf("file not marked as patch file")
	}
}

func TestDeletionMarker(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_delete_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create archive with deletion marker
	mpqPath := filepath.Join(tmpDir, "delete.mpq")
	archive, err := Create(mpqPath, 10)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}

	if err := archive.AddDeleteMarker("Data\\Deleted.txt"); err != nil {
		t.Fatalf("add delete marker: %v", err)
	}
	archive.Close()

	// Read back and verify
	readArchive, err := Open(mpqPath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer readArchive.Close()

	// File should exist but be marked for deletion
	if !readArchive.IsDeleteMarker("Data\\Deleted.txt") {
		t.Errorf("file not marked for deletion")
	}

	// HasFile should return false for deletion markers
	if readArchive.HasFile("Data\\Deleted.txt") {
		t.Errorf("HasFile returned true for deletion marker")
	}
}

func TestPatchChain(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_chain_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create base archive
	baseFile := filepath.Join(tmpDir, "base.txt")
	baseContent := []byte("Base content")
	os.WriteFile(baseFile, baseContent, 0644)

	baseMPQ := filepath.Join(tmpDir, "base.mpq")
	base, _ := Create(baseMPQ, 10)
	base.AddFile(baseFile, "Data\\File.txt")
	base.Close()

	// Create patch archive
	patchFile := filepath.Join(tmpDir, "patch.txt")
	patchContent := []byte("Patched content")
	os.WriteFile(patchFile, patchContent, 0644)

	patchMPQ := filepath.Join(tmpDir, "patch.mpq")
	patch, _ := Create(patchMPQ, 10)
	patch.AddFile(patchFile, "Data\\File.txt")
	patch.Close()

	// Open as patch chain
	chain, err := OpenPatchChain([]string{baseMPQ, patchMPQ})
	if err != nil {
		t.Fatalf("open patch chain: %v", err)
	}
	defer chain.Close()

	// Extract should get patched version
	extractPath := filepath.Join(tmpDir, "extracted.txt")
	if err := chain.ExtractFile("Data\\File.txt", extractPath); err != nil {
		t.Fatalf("extract from chain: %v", err)
	}

	extracted, _ := os.ReadFile(extractPath)
	if string(extracted) != string(patchContent) {
		t.Errorf("got base content, expected patch content")
	}
}

func TestPatchChainDeletionMarker(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_chain_delete_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create base archive with file
	baseFile := filepath.Join(tmpDir, "base.txt")
	os.WriteFile(baseFile, []byte("Base content"), 0644)

	baseMPQ := filepath.Join(tmpDir, "base.mpq")
	base, _ := Create(baseMPQ, 10)
	base.AddFile(baseFile, "Data\\File.txt")
	base.Close()

	// Create patch archive with deletion marker
	patchMPQ := filepath.Join(tmpDir, "patch.mpq")
	patch, _ := Create(patchMPQ, 10)
	patch.AddDeleteMarker("Data\\File.txt")
	patch.Close()

	// Open as patch chain
	chain, err := OpenPatchChain([]string{baseMPQ, patchMPQ})
	if err != nil {
		t.Fatalf("open patch chain: %v", err)
	}
	defer chain.Close()

	// File should not be found (deleted in patch)
	if chain.HasFile("Data\\File.txt") {
		t.Errorf("file found despite deletion marker in patch")
	}

	// Extract should fail
	extractPath := filepath.Join(tmpDir, "extracted.txt")
	if err := chain.ExtractFile("Data\\File.txt", extractPath); err == nil {
		t.Errorf("extract succeeded for deleted file")
	}
}

// TestModifyArchive tests opening an archive for modification
func TestModifyArchive(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_modify_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create initial archive with some files
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	os.WriteFile(file1, []byte("Original content 1"), 0644)
	os.WriteFile(file2, []byte("Original content 2"), 0644)

	mpqPath := filepath.Join(tmpDir, "test.mpq")
	archive, err := Create(mpqPath, 10)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	archive.AddFile(file1, "Data\\File1.txt")
	archive.AddFile(file2, "Data\\File2.txt")
	if err := archive.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	// Open for modification
	archive, err = OpenForModify(mpqPath)
	if err != nil {
		t.Fatalf("open for modify: %v", err)
	}

	// Add a new file
	file3 := filepath.Join(tmpDir, "file3.txt")
	os.WriteFile(file3, []byte("New file content"), 0644)
	if err := archive.AddFile(file3, "Data\\File3.txt"); err != nil {
		t.Fatalf("add new file: %v", err)
	}

	// Close and save modifications
	if err := archive.Close(); err != nil {
		t.Fatalf("close modified archive: %v", err)
	}

	// Open and verify all three files exist
	archive, err = Open(mpqPath)
	if err != nil {
		t.Fatalf("open modified archive: %v", err)
	}
	defer archive.Close()

	if !archive.HasFile("Data\\File1.txt") {
		t.Errorf("original file 1 missing")
	}
	if !archive.HasFile("Data\\File2.txt") {
		t.Errorf("original file 2 missing")
	}
	if !archive.HasFile("Data\\File3.txt") {
		t.Errorf("new file 3 missing")
	}

	// Verify content
	extractPath := filepath.Join(tmpDir, "extracted3.txt")
	if err := archive.ExtractFile("Data\\File3.txt", extractPath); err != nil {
		t.Fatalf("extract new file: %v", err)
	}
	content, _ := os.ReadFile(extractPath)
	if string(content) != "New file content" {
		t.Errorf("new file content mismatch: got %q, want %q", content, "New file content")
	}
}

// TestModifyRemoveFile tests removing files from an archive
func TestModifyRemoveFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_remove_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create initial archive with files
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	os.WriteFile(file1, []byte("Content 1"), 0644)
	os.WriteFile(file2, []byte("Content 2"), 0644)

	mpqPath := filepath.Join(tmpDir, "test.mpq")
	archive, _ := Create(mpqPath, 10)
	archive.AddFile(file1, "Data\\File1.txt")
	archive.AddFile(file2, "Data\\File2.txt")
	archive.Close()

	// Open for modification and remove a file
	archive, err = OpenForModify(mpqPath)
	if err != nil {
		t.Fatalf("open for modify: %v", err)
	}

	if err := archive.RemoveFile("Data\\File1.txt"); err != nil {
		t.Fatalf("remove file: %v", err)
	}

	if err := archive.Close(); err != nil {
		t.Fatalf("close modified archive: %v", err)
	}

	// Open and verify file1 is gone, file2 remains
	archive, err = Open(mpqPath)
	if err != nil {
		t.Fatalf("open modified archive: %v", err)
	}
	defer archive.Close()

	if archive.HasFile("Data\\File1.txt") {
		t.Errorf("removed file still present")
	}
	if !archive.HasFile("Data\\File2.txt") {
		t.Errorf("remaining file missing")
	}
}

// TestModifyReplaceFile tests replacing an existing file
func TestModifyReplaceFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_replace_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create initial archive
	file1 := filepath.Join(tmpDir, "file1.txt")
	os.WriteFile(file1, []byte("Original content"), 0644)

	mpqPath := filepath.Join(tmpDir, "test.mpq")
	archive, _ := Create(mpqPath, 10)
	archive.AddFile(file1, "Data\\File.txt")
	archive.Close()

	// Open for modification and replace the file
	archive, err = OpenForModify(mpqPath)
	if err != nil {
		t.Fatalf("open for modify: %v", err)
	}

	file1New := filepath.Join(tmpDir, "file1_new.txt")
	os.WriteFile(file1New, []byte("Replaced content"), 0644)
	if err := archive.AddFile(file1New, "Data\\File.txt"); err != nil {
		t.Fatalf("replace file: %v", err)
	}

	if err := archive.Close(); err != nil {
		t.Fatalf("close modified archive: %v", err)
	}

	// Open and verify content is updated
	archive, err = Open(mpqPath)
	if err != nil {
		t.Fatalf("open modified archive: %v", err)
	}
	defer archive.Close()

	extractPath := filepath.Join(tmpDir, "extracted.txt")
	if err := archive.ExtractFile("Data\\File.txt", extractPath); err != nil {
		t.Fatalf("extract file: %v", err)
	}

	content, _ := os.ReadFile(extractPath)
	if string(content) != "Replaced content" {
		t.Errorf("file content mismatch: got %q, want %q", content, "Replaced content")
	}
}

// TestModifyWithCRC tests modifying an archive with CRC files
func TestModifyWithCRC(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_modify_crc_test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create archive with CRC
	file1 := filepath.Join(tmpDir, "file1.txt")
	os.WriteFile(file1, []byte("Content with CRC"), 0644)

	mpqPath := filepath.Join(tmpDir, "test.mpq")
	archive, _ := Create(mpqPath, 10)
	archive.AddFileWithCRC(file1, "Data\\File.txt")
	archive.Close()

	// Modify the archive
	archive, err = OpenForModify(mpqPath)
	if err != nil {
		t.Fatalf("open for modify: %v", err)
	}

	file2 := filepath.Join(tmpDir, "file2.txt")
	os.WriteFile(file2, []byte("New file"), 0644)
	archive.AddFile(file2, "Data\\File2.txt")
	archive.Close()

	// Verify both files work
	archive, err = Open(mpqPath)
	if err != nil {
		t.Fatalf("open modified archive: %v", err)
	}
	defer archive.Close()

	extractPath1 := filepath.Join(tmpDir, "extracted1.txt")
	if err := archive.ExtractFile("Data\\File.txt", extractPath1); err != nil {
		t.Fatalf("extract CRC file: %v", err)
	}
	content1, _ := os.ReadFile(extractPath1)
	if string(content1) != "Content with CRC" {
		t.Errorf("CRC file content mismatch: got %q, want %q", content1, "Content with CRC")
	}

	extractPath2 := filepath.Join(tmpDir, "extracted2.txt")
	if err := archive.ExtractFile("Data\\File2.txt", extractPath2); err != nil {
		t.Fatalf("extract new file: %v", err)
	}
	content2, _ := os.ReadFile(extractPath2)
	if string(content2) != "New file" {
		t.Errorf("new file content mismatch: got %q, want %q", content2, "New file")
	}
}

// TestCRC32Algorithm verifies the CRC32 algorithm matches expected values
func TestCRC32Algorithm(t *testing.T) {
	testCases := []struct {
		data     []byte
		expected uint32
	}{
		{[]byte(""), 0x00000000},
		{[]byte("a"), 0xE8B7BE43},
		{[]byte("abc"), 0x352441C2},
		{[]byte("Hello, World!"), 0xEC4AC3D0},
		{[]byte("The quick brown fox jumps over the lazy dog"), 0x414FA339},
	}

	for _, tc := range testCases {
		got := crc32(tc.data)
		if got != tc.expected {
			t.Errorf("CRC32 mismatch for %q: got 0x%08X, expected 0x%08X",
				tc.data, got, tc.expected)
		}
	}
}

// TestAdler32Algorithm verifies the Adler32 algorithm
func TestAdler32Algorithm(t *testing.T) {
	testCases := []struct {
		data     []byte
		expected uint32
	}{
		{[]byte(""), 0x00000001},
		{[]byte("a"), 0x00620062},
		{[]byte("abc"), 0x024D0127},
		{[]byte("Wikipedia"), 0x11E60398},
	}

	for _, tc := range testCases {
		got := adler32(tc.data)
		if got != tc.expected {
			t.Errorf("Adler32 mismatch for %q: got 0x%08X, expected 0x%08X",
				tc.data, got, tc.expected)
		}
	}
}

// TestMultiSectorFileCRCGeneration tests CRC generation for files spanning multiple sectors
func TestMultiSectorFileCRCGeneration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_multi_sector_crc_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test data that spans multiple sectors (3.25 sectors)
	sectorSize := 4096
	testData := make([]byte, sectorSize*3+1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	testFile := filepath.Join(tmpDir, "large.bin")
	if err := os.WriteFile(testFile, testData, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	// Build archive with CRC generation enabled
	mpqPath := filepath.Join(tmpDir, "test_multi_crc.mpq")
	archive, err := Create(mpqPath, 10)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}

	if err := archive.AddFileWithCRC(testFile, "Data\\Large.bin"); err != nil {
		t.Fatalf("add file with CRC: %v", err)
	}
	archive.Close()

	// Open and verify the archive
	readArchive, err := Open(mpqPath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer readArchive.Close()

	// Read the file - this should validate CRCs for all sectors
	extractPath := filepath.Join(tmpDir, "extracted.bin")
	if err := readArchive.ExtractFile("Data\\Large.bin", extractPath); err != nil {
		t.Fatalf("extract file with CRC: %v", err)
	}

	extracted, _ := os.ReadFile(extractPath)
	if len(extracted) != len(testData) {
		t.Errorf("size mismatch: got %d, want %d", len(extracted), len(testData))
	}

	for i := range testData {
		if extracted[i] != testData[i] {
			t.Fatalf("data mismatch at byte %d", i)
		}
	}

	// Verify CRC flag is set
	block, err := readArchive.findFile("Data\\Large.bin")
	if err != nil {
		t.Fatalf("find file: %v", err)
	}
	if block.Flags&fileSectorCRC == 0 {
		t.Errorf("file should have SECTOR_CRC flag set")
	}
}

// TestNoCRCGeneration tests that archives without CRC generation work correctly
func TestNoCRCGeneration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_no_crc_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test data
	testData := []byte("Test file without CRC")
	testFile := filepath.Join(tmpDir, "no_crc.txt")
	if err := os.WriteFile(testFile, testData, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	// Build archive without CRC generation (default)
	mpqPath := filepath.Join(tmpDir, "test_no_crc.mpq")
	archive, _ := Create(mpqPath, 10)
	archive.AddFile(testFile, "Data\\NoCRC.txt")
	archive.Close()

	// Open and verify the archive
	readArchive, _ := Open(mpqPath)
	defer readArchive.Close()

	// Read the file
	extractPath := filepath.Join(tmpDir, "extracted.txt")
	readArchive.ExtractFile("Data\\NoCRC.txt", extractPath)

	extracted, _ := os.ReadFile(extractPath)
	if string(extracted) != string(testData) {
		t.Errorf("content mismatch")
	}

	// Check file info to verify CRC flag is NOT set
	block, _ := readArchive.findFile("Data\\NoCRC.txt")
	if block.Flags&fileSectorCRC != 0 {
		t.Errorf("file should NOT have SECTOR_CRC flag set")
	}
}

// TestCRCGenerationWithCompression tests CRC generation with compressed files
func TestCRCGenerationWithCompression(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_compressed_crc_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create compressible test data - make it large enough for sectors (>8KB)
	testData := []byte{}
	for i := 0; i < 2000; i++ {
		testData = append(testData, []byte("Hello World! This is compressible data. ")...)
	}
	testFile := filepath.Join(tmpDir, "compressible.txt")
	if err := os.WriteFile(testFile, testData, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	// Build archive with CRC generation and compression enabled
	mpqPath := filepath.Join(tmpDir, "test_compressed_crc.mpq")
	archive, _ := Create(mpqPath, 10)
	archive.AddFileWithCRC(testFile, "Data\\Compressible.txt")
	archive.Close()

	// Open and verify the archive
	readArchive, _ := Open(mpqPath)
	defer readArchive.Close()

	// Read the file - this should decompress and validate CRC
	extractPath := filepath.Join(tmpDir, "extracted.txt")
	err = readArchive.ExtractFile("Data\\Compressible.txt", extractPath)
	if err != nil {
		t.Fatalf("extract file: %v", err)
	}

	extracted, _ := os.ReadFile(extractPath)
	if len(extracted) != len(testData) {
		t.Errorf("size mismatch: got %d, want %d", len(extracted), len(testData))
	}

	// Check file info
	block, _ := readArchive.findFile("Data\\Compressible.txt")
	if block.Flags&fileSectorCRC == 0 {
		t.Errorf("file should have SECTOR_CRC flag set")
	}
	if block.Flags&fileCompress == 0 {
		t.Errorf("file should be compressed")
	}
}

// TestAttributesRoundtrip tests that attributes can be written and read back
func TestAttributesRoundtrip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_attributes_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	file1Data := []byte("File 1 content")
	file2Data := []byte("File 2 content")
	file1Path := filepath.Join(tmpDir, "file1.txt")
	file2Path := filepath.Join(tmpDir, "file2.txt")
	os.WriteFile(file1Path, file1Data, 0644)
	os.WriteFile(file2Path, file2Data, 0644)

	// Build archive
	mpqPath := filepath.Join(tmpDir, "test_attributes.mpq")
	archive, _ := Create(mpqPath, 10)
	archive.AddFile(file1Path, "Data\\File1.txt")
	archive.AddFile(file2Path, "Data\\File2.txt")
	archive.Close()

	// Open and verify attributes exist
	readArchive, _ := Open(mpqPath)
	defer readArchive.Close()

	if !readArchive.HasFile("(attributes)") {
		t.Errorf("archive should contain (attributes) file")
	}

	// Extract attributes and verify it's not empty
	attrPath := filepath.Join(tmpDir, "attributes")
	err = readArchive.ExtractFile("(attributes)", attrPath)
	if err != nil {
		t.Fatalf("failed to extract attributes: %v", err)
	}

	attrData, _ := os.ReadFile(attrPath)
	if len(attrData) < 8 {
		t.Errorf("attributes file too small: %d bytes", len(attrData))
	}
}

// TestPatchChainFileLocation tests tracking which archive contains a file
func TestPatchChainFileLocation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_chain_location_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create base archive with file
	baseFile := filepath.Join(tmpDir, "base.txt")
	os.WriteFile(baseFile, []byte("base content"), 0644)
	baseMPQ := filepath.Join(tmpDir, "base.mpq")
	base, _ := Create(baseMPQ, 10)
	base.AddFile(baseFile, "Data\\Base.txt")
	base.Close()

	// Create patch archive with different file
	patchFile := filepath.Join(tmpDir, "patch.txt")
	os.WriteFile(patchFile, []byte("patch content"), 0644)
	patchMPQ := filepath.Join(tmpDir, "patch.mpq")
	patch, _ := Create(patchMPQ, 10)
	patch.AddFile(patchFile, "Data\\Patch.txt")
	patch.Close()

	// Open as patch chain
	chain, err := OpenPatchChain([]string{baseMPQ, patchMPQ})
	if err != nil {
		t.Fatalf("open patch chain: %v", err)
	}
	defer chain.Close()

	// Verify both files are accessible
	if !chain.HasFile("Data\\Base.txt") {
		t.Errorf("base file should be accessible")
	}
	if !chain.HasFile("Data\\Patch.txt") {
		t.Errorf("patch file should be accessible")
	}
	if chain.HasFile("Data\\Nonexistent.txt") {
		t.Errorf("nonexistent file should not be found")
	}
}

// TestPatchChainListing tests listing unique files across patch chain
func TestPatchChainListing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_chain_listing_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create base archive
	base1 := filepath.Join(tmpDir, "file1.txt")
	base2 := filepath.Join(tmpDir, "file2.txt")
	os.WriteFile(base1, []byte("1"), 0644)
	os.WriteFile(base2, []byte("2"), 0644)
	baseMPQ := filepath.Join(tmpDir, "base.mpq")
	base, _ := Create(baseMPQ, 10)
	base.AddFile(base1, "Data\\File1.txt")
	base.AddFile(base2, "Data\\File2.txt")
	base.Close()

	// Create patch archive (overrides file2, adds file3)
	patch2 := filepath.Join(tmpDir, "file2_patched.txt")
	patch3 := filepath.Join(tmpDir, "file3.txt")
	os.WriteFile(patch2, []byte("2-patched"), 0644)
	os.WriteFile(patch3, []byte("3"), 0644)
	patchMPQ := filepath.Join(tmpDir, "patch.mpq")
	patch, _ := Create(patchMPQ, 10)
	patch.AddFile(patch2, "Data\\File2.txt")
	patch.AddFile(patch3, "Data\\File3.txt")
	patch.Close()

	// Open as patch chain
	chain, err := OpenPatchChain([]string{baseMPQ, patchMPQ})
	if err != nil {
		t.Fatalf("open patch chain: %v", err)
	}
	defer chain.Close()

	// List files
	files, err := chain.ListFiles()
	if err != nil {
		t.Fatalf("list files: %v", err)
	}

	// Filter out special files
	var userFiles []string
	for _, f := range files {
		if f != "(listfile)" && f != "(attributes)" {
			userFiles = append(userFiles, f)
		}
	}

	// Should have 3 unique files
	if len(userFiles) != 3 {
		t.Errorf("expected 3 files, got %d: %v", len(userFiles), userFiles)
	}

	// Check all files are present
	hasFile1 := false
	hasFile2 := false
	hasFile3 := false
	for _, f := range userFiles {
		switch f {
		case "Data\\File1.txt":
			hasFile1 = true
		case "Data\\File2.txt":
			hasFile2 = true
		case "Data\\File3.txt":
			hasFile3 = true
		}
	}

	if !hasFile1 || !hasFile2 || !hasFile3 {
		t.Errorf("missing files: file1=%v, file2=%v, file3=%v", hasFile1, hasFile2, hasFile3)
	}
}

// TestMultiplePatchChain tests a chain with multiple patches
func TestMultiplePatchChain(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mpq_multi_patch_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create base archive
	createVersionArchive := func(name, version string) string {
		versionFile := filepath.Join(tmpDir, name+".txt")
		os.WriteFile(versionFile, []byte(version), 0644)
		mpqPath := filepath.Join(tmpDir, name+".mpq")
		archive, _ := Create(mpqPath, 10)
		archive.AddFile(versionFile, "Data\\Version.txt")
		archive.Close()
		return mpqPath
	}

	basePath := createVersionArchive("base", "1.0.0")
	patch1Path := createVersionArchive("patch-1", "1.1.0")
	patch2Path := createVersionArchive("patch-2", "1.2.0")
	patch3Path := createVersionArchive("patch-3", "1.3.0")

	// Build chain with all patches
	chain, err := OpenPatchChain([]string{basePath, patch1Path, patch2Path, patch3Path})
	if err != nil {
		t.Fatalf("open patch chain: %v", err)
	}
	defer chain.Close()

	// Highest priority patch should win
	extractPath := filepath.Join(tmpDir, "version.txt")
	chain.ExtractFile("Data\\Version.txt", extractPath)
	version, _ := os.ReadFile(extractPath)

	if string(version) != "1.3.0" {
		t.Errorf("expected version 1.3.0, got %s", version)
	}

	// Verify chain has correct number of archives
	if chain.GetArchiveCount() != 4 {
		t.Errorf("expected 4 archives in chain, got %d", chain.GetArchiveCount())
	}
}
