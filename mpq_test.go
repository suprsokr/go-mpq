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
