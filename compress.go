// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

import (
	"bytes"
	"compress/bzip2"
	"compress/zlib"
	"fmt"
	"io"
)

// Compression type constants
const (
	compressionHuffman   = 0x01 // Huffman (used on wave files only)
	compressionZlib      = 0x02 // Zlib compression
	compressionPKWare    = 0x08 // PKWare DCL compression
	compressionBzip2     = 0x10 // BZip2 compression
	compressionSparse    = 0x20 // Sparse/RLE compression (SC2+)
	compressionADPCMMono = 0x40 // ADPCM mono audio
	compressionADPCM     = 0x80 // ADPCM stereo audio
	compressionLZMA      = 0x12 // LZMA compression (SC2+)
)

// compressData compresses data using zlib
func compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer

	// Write compression type byte
	buf.WriteByte(compressionZlib)

	// Compress with zlib
	w, err := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("create zlib writer: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("zlib write: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("zlib close: %w", err)
	}

	return buf.Bytes(), nil
}

// decompressData decompresses MPQ-compressed data
// Supports multi-compression: compressions are applied in order and must be
// decompressed in reverse order (last compression first)
func decompressData(data []byte, uncompressedSize uint32) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty compressed data")
	}

	// First byte is compression type (can be a bitmask for multi-compression)
	compressionType := data[0]
	data = data[1:]

	// Handle multi-compression by processing in reverse order
	// Order of decompression (reverse of compression order):
	// 1. BZip2 or Zlib or PKWare or LZMA (primary compression)
	// 2. Sparse (if present)
	// 3. Huffman (if present) 
	// 4. ADPCM (if present, for audio)

	result := data

	// Primary decompression
	switch {
	case compressionType == compressionZlib:
		return decompressZlib(result, uncompressedSize)

	case compressionType == compressionPKWare:
		return decompressPKWare(result, uncompressedSize)

	case compressionType == compressionBzip2:
		return decompressBzip2(result, uncompressedSize)

	case compressionType == compressionLZMA:
		return nil, fmt.Errorf("LZMA compression not supported")

	case compressionType == compressionHuffman:
		return nil, fmt.Errorf("Huffman-only compression not supported")

	case compressionType == compressionADPCMMono:
		return nil, fmt.Errorf("ADPCM mono compression not supported")

	case compressionType == compressionADPCM:
		return nil, fmt.Errorf("ADPCM stereo compression not supported")

	default:
		// Multi-compression: check for combinations
		var err error

		// Decompress in reverse order of compression

		// Step 1: Primary compression (Zlib, BZip2, PKWare)
		if compressionType&compressionBzip2 != 0 {
			result, err = decompressBzip2(result, uncompressedSize)
			if err != nil {
				return nil, fmt.Errorf("multi bzip2: %w", err)
			}
		} else if compressionType&compressionZlib != 0 {
			result, err = decompressZlib(result, uncompressedSize)
			if err != nil {
				return nil, fmt.Errorf("multi zlib: %w", err)
			}
		} else if compressionType&compressionPKWare != 0 {
			result, err = decompressPKWare(result, uncompressedSize)
			if err != nil {
				return nil, fmt.Errorf("multi pkware: %w", err)
			}
		}

		// Step 2: Huffman (typically applied before primary compression)
		if compressionType&compressionHuffman != 0 {
			// Huffman is usually combined with ADPCM for wave files
			// For now, we don't support standalone Huffman
			if compressionType&(compressionADPCMMono|compressionADPCM) == 0 {
				return nil, fmt.Errorf("Huffman compression without ADPCM not supported")
			}
		}

		// Step 3: ADPCM decompression (for wave files)
		if compressionType&compressionADPCMMono != 0 {
			return nil, fmt.Errorf("ADPCM mono compression not supported")
		}
		if compressionType&compressionADPCM != 0 {
			return nil, fmt.Errorf("ADPCM stereo compression not supported")
		}

		if len(result) == 0 {
			return nil, fmt.Errorf("unsupported compression type: 0x%02X", compressionType)
		}

		return result, nil
	}
}

// decompressZlib decompresses zlib-compressed data
func decompressZlib(data []byte, uncompressedSize uint32) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create zlib reader: %w", err)
	}
	defer r.Close()

	result := make([]byte, uncompressedSize)
	n, err := io.ReadFull(r, result)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("zlib decompress: %w", err)
	}

	return result[:n], nil
}

// decompressBzip2 decompresses bzip2-compressed data
func decompressBzip2(data []byte, uncompressedSize uint32) ([]byte, error) {
	r := bzip2.NewReader(bytes.NewReader(data))

	result := make([]byte, uncompressedSize)
	n, err := io.ReadFull(r, result)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("bzip2 decompress: %w", err)
	}

	return result[:n], nil
}
