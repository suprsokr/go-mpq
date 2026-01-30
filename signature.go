// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

import (
	"encoding/binary"
	"fmt"
)

// SignatureInfo contains parsed signature data from (signature) file.
type SignatureInfo struct {
	Version   uint32
	Signature []byte
}

// ReadSignature reads and parses the (signature) special file if present.
// Returns nil if the signature file doesn't exist.
func (a *Archive) ReadSignature() (*SignatureInfo, error) {
	if a.mode != "r" {
		return nil, fmt.Errorf("archive not opened for reading")
	}

	// Check if signature file exists
	block, err := a.findFile("(signature)")
	if err != nil {
		return nil, nil // Signature is optional
	}

	// Read signature file data
	blockPos := block.getFilePos64()
	filePos := blockPos + a.header.ArchiveOffset
	if _, err := a.file.Seek(int64(filePos), 0); err != nil {
		return nil, fmt.Errorf("seek to signature data: %w", err)
	}

	compressedData := make([]byte, block.CompressedSize)
	if n, err := a.file.Read(compressedData); err != nil || n != int(block.CompressedSize) {
		return nil, fmt.Errorf("read signature data: %w", err)
	}

	var signatureData []byte

	// Decompress if needed
	if block.Flags&fileCompress != 0 && block.CompressedSize < block.FileSize {
		decompressed, err := decompressData(compressedData, block.FileSize)
		if err != nil {
			return nil, fmt.Errorf("decompress signature: %w", err)
		}
		signatureData = decompressed
	} else {
		signatureData = compressedData
	}

	if len(signatureData) < 8 {
		return nil, fmt.Errorf("signature data too small: %d bytes", len(signatureData))
	}

	// Parse signature structure
	version := binary.LittleEndian.Uint32(signatureData[0:4])
	sigLength := binary.LittleEndian.Uint32(signatureData[4:8])

	if len(signatureData) < int(8+sigLength) {
		return nil, fmt.Errorf("signature data truncated: expected %d bytes, got %d", 8+sigLength, len(signatureData))
	}

	signature := make([]byte, sigLength)
	copy(signature, signatureData[8:8+sigLength])

	return &SignatureInfo{
		Version:   version,
		Signature: signature,
	}, nil
}

// VerifySignature performs basic signature validation.
// Note: This is a placeholder for full cryptographic verification.
// In practice, you would verify the signature against the archive data using
// the appropriate public key and signature algorithm (typically RSA or similar).
func (s *SignatureInfo) VerifySignature(archiveData []byte) error {
	if s == nil {
		return fmt.Errorf("no signature available")
	}

	if len(s.Signature) == 0 {
		return fmt.Errorf("empty signature")
	}

	// Basic validation - check signature is present and has reasonable size
	// Real implementation would:
	// 1. Extract public key (from signature version/type)
	// 2. Compute hash of archive data (excluding signature itself)
	// 3. Verify RSA/DSA signature using public key
	//
	// This is left as a stub since full crypto verification requires
	// knowledge of Blizzard's specific signature format and public keys.

	switch s.Version {
	case 0: // Weak signature (deprecated)
		if len(s.Signature) < 64 {
			return fmt.Errorf("weak signature too short: %d bytes", len(s.Signature))
		}
	case 1: // Strong signature
		if len(s.Signature) < 256 {
			return fmt.Errorf("strong signature too short: %d bytes", len(s.Signature))
		}
	default:
		return fmt.Errorf("unknown signature version: %d", s.Version)
	}

	// Placeholder: return success for now
	// Real implementation would return error if signature verification fails
	return nil
}
