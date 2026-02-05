// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

package mpq

import (
	"fmt"
	"path/filepath"
	"strings"
)

// normalizeMpqPath normalizes a path for MPQ lookup.
// Converts forward slashes to backslashes and normalizes case.
// This matches MPQ's internal path handling (case-insensitive, backslash separators).
func normalizeMpqPath(path string) string {
	// Replace forward slashes with backslashes
	normalized := strings.ReplaceAll(path, "/", "\\")
	// Convert to uppercase for case-insensitive matching
	normalized = strings.ToUpper(normalized)
	// Clean up any double backslashes (though MPQ shouldn't have these)
	for strings.Contains(normalized, "\\\\") {
		normalized = strings.ReplaceAll(normalized, "\\\\", "\\")
	}
	return normalized
}

// PatchChain represents a prioritized list of MPQ archives.
type PatchChain struct {
	archives   []*Archive
	metadata   map[string]*PatchMetadata // metadata per archive path
	fileMap    map[string]int            // cache: normalized filename -> archive index
	cacheBuilt bool                      // whether fileMap has been populated
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

	chain := &PatchChain{
		archives:   archives,
		metadata:   metadata,
		fileMap:    make(map[string]int),
		cacheBuilt: false,
	}

	// Build cache eagerly (non-fatal if it fails)
	if err := chain.rebuildFileMap(); err != nil {
		// Cache build can fail if archives don't have listfiles
		// Cache will be built lazily on first lookup
	}

	return chain, nil
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
	// Ensure cache is built
	if !p.cacheBuilt {
		if err := p.rebuildFileMap(); err != nil {
			// Fall back to linear search if cache build fails
			return p.hasFileLinear(mpqPath)
		}
	}

	normalizedPath := normalizeMpqPath(mpqPath)

	// Check cache first
	archiveIdx, found := p.fileMap[normalizedPath]
	if !found {
		return false
	}

	// Verify file still exists and check for deletion marker
	archive := p.archives[archiveIdx]
	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")
	block, err := archive.findFile(mpqPath)
	if err != nil {
		// File removed? Rebuild cache
		p.rebuildFileMap()
		return false
	}

	// Check for deletion marker
	if block.Flags&fileDeleteMarker != 0 {
		return false
	}

	return true
}

// hasFileLinear is the fallback linear search implementation.
func (p *PatchChain) hasFileLinear(mpqPath string) bool {
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
	// Ensure cache is built
	if !p.cacheBuilt {
		if err := p.rebuildFileMap(); err != nil {
			// Fall back to linear search
			return p.extractFileLinear(mpqPath, destPath)
		}
	}

	normalizedPath := normalizeMpqPath(mpqPath)

	// Check cache
	archiveIdx, found := p.fileMap[normalizedPath]
	if !found {
		return fmt.Errorf("file not found in patch chain: %s", mpqPath)
	}

	// Extract from the cached archive
	archive := p.archives[archiveIdx]
	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")
	block, err := archive.findFile(mpqPath)
	if err != nil {
		// File removed? Rebuild cache and retry
		p.rebuildFileMap()
		return fmt.Errorf("file not found in patch chain: %s", mpqPath)
	}

	// Check for deletion marker
	if block.Flags&fileDeleteMarker != 0 {
		return fmt.Errorf("file marked for deletion in patch: %s", mpqPath)
	}

	return archive.ExtractFile(mpqPath, destPath)
}

// extractFileLinear is the fallback linear search implementation.
func (p *PatchChain) extractFileLinear(mpqPath, destPath string) error {
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
	// Ensure cache is built (though we still need to search all archives)
	if !p.cacheBuilt {
		if err := p.rebuildFileMap(); err != nil {
			return p.hasPatchFileLinear(mpqPath)
		}
	}

	// For patch files, we need to check all archives since patch files
	// can exist in multiple archives, not just the highest priority one
	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")
	for i := len(p.archives) - 1; i >= 0; i-- {
		block, err := p.archives[i].findFile(mpqPath)
		if err == nil && block.Flags&filePatchFile != 0 {
			return true
		}
	}
	return false
}

// hasPatchFileLinear is the fallback linear search implementation.
func (p *PatchChain) hasPatchFileLinear(mpqPath string) bool {
	mpqPath = strings.ReplaceAll(mpqPath, "/", "\\")
	for i := len(p.archives) - 1; i >= 0; i-- {
		block, err := p.archives[i].findFile(mpqPath)
		if err == nil && block.Flags&filePatchFile != 0 {
			return true
		}
	}
	return false
}

// rebuildFileMap rebuilds the internal file map cache.
// This should be called when archives are added/removed.
func (p *PatchChain) rebuildFileMap() error {
	p.fileMap = make(map[string]int)

	// Process archives in reverse order (highest priority first)
	// This ensures higher-priority archives override lower-priority ones
	for i := len(p.archives) - 1; i >= 0; i-- {
		archive := p.archives[i]

		// Get list of files in this archive
		files, err := archive.ListFiles()
		if err != nil {
			// If ListFiles fails, try to continue with other archives
			// This handles archives without listfiles gracefully
			continue
		}

		// Add files to map (only if not already present from higher priority)
		for _, file := range files {
			key := normalizeMpqPath(file)
			// Only add if not already in map (higher priority archives processed first)
			if _, exists := p.fileMap[key]; !exists {
				p.fileMap[key] = i
			}
		}
	}

	p.cacheBuilt = true
	return nil
}
