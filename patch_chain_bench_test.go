// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkPatchChainLookup benchmarks file lookup performance with cache
func BenchmarkPatchChainLookup(b *testing.B) {
	tmpDir := b.TempDir()

	// Create multiple archives to simulate a real patch chain
	var archivePaths []string
	for i := 0; i < 5; i++ {
		archivePath := filepath.Join(tmpDir, "archive_"+string(rune('0'+i))+".mpq")
		archive, err := Create(archivePath, 50)
		if err != nil {
			b.Fatal(err)
		}

		// Add some files to each archive
		for j := 0; j < 20; j++ {
			fileName := filepath.Join(tmpDir, "file_"+string(rune('a'+j))+".txt")
			content := []byte("test content " + string(rune('0'+i)) + string(rune('a'+j)))
			if err := os.WriteFile(fileName, content, 0644); err != nil {
				b.Fatal(err)
			}

			mpqPath := "Data\\File_" + string(rune('a'+j)) + ".txt"
			if err := archive.AddFile(fileName, mpqPath); err != nil {
				b.Fatal(err)
			}
		}

		if err := archive.Close(); err != nil {
			b.Fatal(err)
		}

		archivePaths = append(archivePaths, archivePath)
	}

	// Open patch chain
	chain, err := OpenPatchChain(archivePaths)
	if err != nil {
		b.Fatal(err)
	}
	defer chain.Close()

	// Reset timer after setup
	b.ResetTimer()

	// Benchmark file lookups
	for i := 0; i < b.N; i++ {
		// Test various file lookups
		chain.HasFile("Data\\File_a.txt")
		chain.HasFile("Data\\File_j.txt")
		chain.HasFile("Data\\File_t.txt")
		chain.HasFile("Data\\NonExistent.txt")
	}
}

// BenchmarkPatchChainLinearLookup benchmarks file lookup with linear search
func BenchmarkPatchChainLinearLookup(b *testing.B) {
	tmpDir := b.TempDir()

	// Create multiple archives
	var archivePaths []string
	for i := 0; i < 5; i++ {
		archivePath := filepath.Join(tmpDir, "archive_"+string(rune('0'+i))+".mpq")
		archive, err := Create(archivePath, 50)
		if err != nil {
			b.Fatal(err)
		}

		// Add some files to each archive
		for j := 0; j < 20; j++ {
			fileName := filepath.Join(tmpDir, "file_"+string(rune('a'+j))+".txt")
			content := []byte("test content " + string(rune('0'+i)) + string(rune('a'+j)))
			if err := os.WriteFile(fileName, content, 0644); err != nil {
				b.Fatal(err)
			}

			mpqPath := "Data\\File_" + string(rune('a'+j)) + ".txt"
			if err := archive.AddFile(fileName, mpqPath); err != nil {
				b.Fatal(err)
			}
		}

		if err := archive.Close(); err != nil {
			b.Fatal(err)
		}

		archivePaths = append(archivePaths, archivePath)
	}

	// Open patch chain
	chain, err := OpenPatchChain(archivePaths)
	if err != nil {
		b.Fatal(err)
	}
	defer chain.Close()

	// Reset timer after setup
	b.ResetTimer()

	// Benchmark using linear search fallback
	for i := 0; i < b.N; i++ {
		// Test various file lookups
		chain.hasFileLinear("Data\\File_a.txt")
		chain.hasFileLinear("Data\\File_j.txt")
		chain.hasFileLinear("Data\\File_t.txt")
		chain.hasFileLinear("Data\\NonExistent.txt")
	}
}

// BenchmarkPatchChainExtract benchmarks file extraction with cache
func BenchmarkPatchChainExtract(b *testing.B) {
	tmpDir := b.TempDir()

	// Create archives
	var archivePaths []string
	for i := 0; i < 3; i++ {
		archivePath := filepath.Join(tmpDir, "archive_"+string(rune('0'+i))+".mpq")
		archive, err := Create(archivePath, 30)
		if err != nil {
			b.Fatal(err)
		}

		// Add files
		for j := 0; j < 10; j++ {
			fileName := filepath.Join(tmpDir, "file_"+string(rune('a'+j))+".txt")
			content := []byte("test content " + string(rune('0'+i)) + string(rune('a'+j)))
			if err := os.WriteFile(fileName, content, 0644); err != nil {
				b.Fatal(err)
			}

			mpqPath := "Data\\File_" + string(rune('a'+j)) + ".txt"
			if err := archive.AddFile(fileName, mpqPath); err != nil {
				b.Fatal(err)
			}
		}

		if err := archive.Close(); err != nil {
			b.Fatal(err)
		}

		archivePaths = append(archivePaths, archivePath)
	}

	chain, err := OpenPatchChain(archivePaths)
	if err != nil {
		b.Fatal(err)
	}
	defer chain.Close()

	outputDir := filepath.Join(tmpDir, "output")
	os.MkdirAll(outputDir, 0755)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		destPath := filepath.Join(outputDir, "extracted.txt")
		chain.ExtractFile("Data\\File_a.txt", destPath)
		os.Remove(destPath) // Clean up
	}
}
