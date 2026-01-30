// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PatchChain represents a prioritized list of MPQ archives.
type PatchChain struct {
	archives []*Archive
	metadata map[string]*PatchMetadata // metadata per archive path
}

// OpenPatchChain opens multiple MPQ archives in order of increasing priority.
// The last archive in the list has the highest priority.
func OpenPatchChain(paths []string) (*PatchChain, error) {
	archives := make([]*Archive, 0, len(paths))
	metadata := make(map[string]*PatchMetadata)

	for _, path := range paths {
		archive, err := Open(path)
		if err != nil {
			for _, opened := range archives {
				_ = opened.Close()
			}
			return nil, fmt.Errorf("open archive %s: %w", path, err)
		}
		archives = append(archives, archive)

		// Try to read patch metadata if present
		if meta, err := archive.readPatchMetadata(); err == nil && meta != nil {
			metadata[path] = meta
		}
	}

	return &PatchChain{
		archives: archives,
		metadata: metadata,
	}, nil
}

// Close closes all archives in the patch chain.
func (p *PatchChain) Close() error {
	var firstErr error
	for _, archive := range p.archives {
		if err := archive.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// HasFile returns true if any archive contains the specified file.
// Respects deletion markers in higher-priority archives.
func (p *PatchChain) HasFile(mpqPath string) bool {
	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")
	for i := len(p.archives) - 1; i >= 0; i-- {
		archive := p.archives[i]
		block, err := archive.findFile(mpqPath)
		if err == nil {
			// If file exists, check if it's a deletion marker
			if block.Flags&fileDeleteMarker != 0 {
				return false // File marked for deletion
			}
			return true // File exists and not deleted
		}
	}
	return false
}

// ExtractFile extracts the highest-priority version of a file.
// Respects deletion markers in patch archives.
func (p *PatchChain) ExtractFile(mpqPath, destPath string) error {
	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")
	for i := len(p.archives) - 1; i >= 0; i-- {
		archive := p.archives[i]
		block, err := archive.findFile(mpqPath)
		if err == nil {
			// Check for deletion marker
			if block.Flags&fileDeleteMarker != 0 {
				return fmt.Errorf("file marked for deletion in patch: %s", mpqPath)
			}
			return archive.ExtractFile(mpqPath, destPath)
		}
	}
	return fmt.Errorf("file not found in patch chain: %s", mpqPath)
}

// ListFiles returns the union of listfiles across the chain.
func (p *PatchChain) ListFiles() ([]string, error) {
	seen := make(map[string]struct{})
	var result []string
	for _, archive := range p.archives {
		files, err := archive.ListFiles()
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			key := strings.ToLower(filepath.Clean(strings.ReplaceAll(file, "/", "\\")))
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, file)
		}
	}
	return result, nil
}

// GetPatchMetadata returns the patch metadata for a specific archive in the chain.
func (p *PatchChain) GetPatchMetadata(archivePath string) *PatchMetadata {
	return p.metadata[archivePath]
}

// GetArchiveCount returns the number of archives in the chain.
func (p *PatchChain) GetArchiveCount() int {
	return len(p.archives)
}

// HasPatchFile checks if a file is marked as a patch file in any archive.
func (p *PatchChain) HasPatchFile(mpqPath string) bool {
	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")
	for i := len(p.archives) - 1; i >= 0; i-- {
		block, err := p.archives[i].findFile(mpqPath)
		if err == nil && block.Flags&filePatchFile != 0 {
			return true
		}
	}
	return false
}
