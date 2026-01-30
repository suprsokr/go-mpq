# go-mpq

[![Go Reference](https://pkg.go.dev/badge/github.com/suprsokr/go-mpq.svg)](https://pkg.go.dev/github.com/suprsokr/go-mpq)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Pure Go library for reading and writing MPQ (Mo'PaQ) archives.

MPQ is an archive format created by Blizzard Entertainment, used in games like Diablo, StarCraft, and World of Warcraft. This package supports MPQ format versions 1 and 2, covering games up through WoW: Wrath of the Lich King (3.3.5a).

## Features

- **Pure Go** - No CGO, no external dependencies
- **Cross-platform** - Works on Windows, macOS, and Linux
- **Read & Write** - Create new archives and extract from existing ones
- **Archive Modification** - Add, remove, and replace files in existing archives
- **V1 & V2 Support** - Original format and Burning Crusade extended format
- **Zlib Compression** - Automatic compression for smaller archives
- **Sector CRC** - Generate and validate sector checksums (ADLER32)
- **Patch Chain** - Multi-archive overlay support with deletion markers
- **Signature Support** - Read digital signatures from archives
- **User Data Header** - Scan for embedded MPQ archives (MPQ\x1B)

## Installation

```bash
go get github.com/suprsokr/go-mpq
```

## Quick Start

### Creating an Archive

```go
package main

import (
    "log"
    "github.com/suprsokr/go-mpq"
)

func main() {
    // Create a new archive (V1 format)
    archive, err := mpq.Create("patch.mpq", 100)
    if err != nil {
        log.Fatal(err)
    }
    defer archive.Close()

    // Add files
    err = archive.AddFile("local/spell.dbc", "DBFilesClient\\Spell.dbc")
    if err != nil {
        log.Fatal(err)
    }

    err = archive.AddFile("local/ui.lua", "Interface\\AddOns\\MyAddon\\ui.lua")
    if err != nil {
        log.Fatal(err)
    }
}
```

### Reading an Archive

```go
package main

import (
    "log"
    "github.com/suprsokr/go-mpq"
)

func main() {
    // Open existing archive
    archive, err := mpq.Open("game.mpq")
    if err != nil {
        log.Fatal(err)
    }
    defer archive.Close()

    // Check if file exists
    if archive.HasFile("Data\\file.txt") {
        // Extract file
        err = archive.ExtractFile("Data\\file.txt", "output/file.txt")
        if err != nil {
            log.Fatal(err)
        }
    }
}
```

### Modifying an Archive

```go
package main

import (
    "log"
    "github.com/suprsokr/go-mpq"
)

func main() {
    // Open existing archive for modification
    archive, err := mpq.OpenForModify("game.mpq")
    if err != nil {
        log.Fatal(err)
    }
    defer archive.Close()

    // Add a new file
    err = archive.AddFile("local/newfile.dbc", "DBFilesClient\\NewFile.dbc")
    if err != nil {
        log.Fatal(err)
    }

    // Replace an existing file
    err = archive.AddFile("local/updated.dbc", "DBFilesClient\\Existing.dbc")
    if err != nil {
        log.Fatal(err)
    }

    // Remove a file
    err = archive.RemoveFile("Interface\\OldUI\\frame.xml")
    if err != nil {
        log.Fatal(err)
    }

    // Changes are saved when Close() is called
}
```

### Using V2 Format

For archives that may exceed 4GB or for better compatibility with WoW: TBC and later:

```go
// Create V2 format archive
archive, err := mpq.CreateV2("large-patch.mpq", 1000)
```

### Adding Files with Sector CRC

Enable sector CRC validation for critical files:

```go
// Add file with sector CRC generation
archive, _ := mpq.Create("patch.mpq", 100)
err := archive.AddFileWithCRC("local/data.dbc", "DBFilesClient\\Data.dbc")
```

### Working with Patch Archives

Create and use patch archives with file overrides and deletion markers:

```go
// Create a patch archive
patch, _ := mpq.Create("patch-1.mpq", 50)

// Add modified file
patch.AddPatchFile("modified/spell.dbc", "DBFilesClient\\Spell.dbc")

// Mark a file for deletion
patch.AddDeleteMarker("Interface\\OldUI\\frame.xml")
patch.Close()

// Open as patch chain (base + patches)
chain, err := mpq.OpenPatchChain([]string{
    "base.mpq",
    "patch-1.mpq",
    "patch-2.mpq",
})
defer chain.Close()

// Extract highest-priority version of file
chain.ExtractFile("DBFilesClient\\Spell.dbc", "output/spell.dbc")
```

### Reading Signatures

Check archive signatures (if present):

```go
archive, _ := mpq.Open("game.mpq")
defer archive.Close()

sig, err := archive.ReadSignature()
if sig != nil {
    fmt.Printf("Archive signed with version %d\n", sig.Version)
}
```

## API Reference

### Functions

| Function | Description |
|----------|-------------|
| `Create(path, maxFiles)` | Create new V1 format archive |
| `CreateV2(path, maxFiles)` | Create new V2 format archive |
| `CreateWithVersion(path, maxFiles, version)` | Create archive with specific version |
| `Open(path)` | Open existing archive for reading |
| `OpenForModify(path)` | Open existing archive for modification |

### Archive Methods

| Method | Description |
|--------|-------------|
| `AddFile(srcPath, mpqPath)` | Add file to archive (write/modify mode) |
| `AddFileWithCRC(srcPath, mpqPath)` | Add file with sector CRC generation |
| `AddPatchFile(srcPath, mpqPath)` | Add file marked as patch file |
| `AddDeleteMarker(mpqPath)` | Add deletion marker for patch archives |
| `RemoveFile(mpqPath)` | Remove file from archive (modify mode only) |
| `ExtractFile(mpqPath, destPath)` | Extract file from archive (read/modify mode) |
| `HasFile(mpqPath)` | Check if file exists (respects deletion markers) |
| `IsDeleteMarker(mpqPath)` | Check if file is marked for deletion |
| `IsPatchFile(mpqPath)` | Check if file is marked as patch file |
| `ReadSignature()` | Read digital signature if present |
| `ListFiles()` | List all files in archive |
| `Close()` | Close archive (writes if in write/modify mode) |

### Patch Chain Methods

| Method | Description |
|--------|-------------|
| `OpenPatchChain(paths)` | Open multiple archives as patch chain |
| `HasFile(mpqPath)` | Check if file exists (respects overrides and deletions) |
| `ExtractFile(mpqPath, destPath)` | Extract highest-priority version |
| `ListFiles()` | List unique files across all archives |
| `GetPatchMetadata(archivePath)` | Get patch metadata for archive |
| `HasPatchFile(mpqPath)` | Check if file is marked as patch file |
| `Close()` | Close all archives in chain |

### Path Conventions

MPQ archives use backslash (`\`) as path separator. This library accepts both:

```go
archive.AddFile("src.txt", "Data\\SubDir\\file.txt")  // Native MPQ style
archive.AddFile("src.txt", "Data/SubDir/file.txt")    // Also works
```

## Format Versions

| Version | Header Size | Max Size | Games |
|---------|-------------|----------|-------|
| V1 | 32 bytes | 4 GB | Diablo, WC3, WoW Classic |
| V2 | 44 bytes | >4 GB | WoW: TBC, WotLK (3.3.5a) |

## Feature Support

This library provides comprehensive MPQ v1 and v2 format support for games through World of Warcraft: Wrath of the Lich King (3.3.5a).

### Core Format Support

| Feature | Status | Notes |
|---------|:------:|-------|
| MPQ v1 (up to 4GB) | ✅ | Original format - Diablo, WC3, WoW Classic |
| MPQ v2 (>4GB) | ✅ | Extended format with 64-bit offsets |
| User data headers (MPQ\x1B) | ✅ | Scan for embedded archives |
| Hi-block table (v2) | ✅ | 64-bit file position support |
| Single-unit files | ✅ | Small files stored as one block |
| Multi-sector files | ✅ | Large files with sector offset tables |
| Sector size configuration | ✅ | Configurable via SectorSizeShift |

### Archive Operations

| Operation | Read | Write | Notes |
|-----------|:----:|:-----:|-------|
| Open/Create V1 archive | ✅ | ✅ | Original format, up to 4GB |
| Open/Create V2 archive | ✅ | ✅ | Extended format, >4GB support |
| Add files | - | ✅ | Create/modify mode |
| Replace files | - | ✅ | Modify mode - add with same path |
| Remove files | - | ✅ | Modify mode - RemoveFile() |
| Extract files | ✅ | - | Single-unit and sectored |
| List files | ✅ | ✅ | Via (listfile), auto-generated on write |
| Encryption (decrypt) | ✅ | ❌ | Read encrypted files only |
| Modify existing archive | ✅ | ✅ | OpenForModify() - add/remove/replace files |
| Compact/rebuild archive | ✅ | ✅ | Automatic on modify - removes deleted space |

### Compression Support

| Method | Read | Write | Notes |
|--------|:----:|:-----:|-------|
| Zlib (0x02) | ✅ | ✅ | Most common in WoW, primary compression |
| PKWare DCL (0x08) | ✅ | ❌ | Legacy Diablo/WC3 archives |
| BZip2 (0x10) | ✅ | ❌ | Some WC3+ files |
| Multi-compression | ✅ | ❌ | Chained algorithms (read-only) |
| Huffman (0x01) | ❌ | ❌ | Audio files only, not implemented |
| ADPCM Mono (0x40) | ❌ | ❌ | Audio files only, not implemented |
| ADPCM Stereo (0x80) | ❌ | ❌ | Audio files only, not implemented |
| Sparse/RLE (0x20) | ❌ | ❌ | StarCraft II+, not needed for v1/v2 |
| LZMA (0x12) | ❌ | ❌ | StarCraft II+, not needed for v1/v2 |

### Checksums & Validation

| Feature | Status | Notes |
|---------|:------:|-------|
| CRC32 algorithm | ✅ | Standard CRC32, verified with test vectors |
| Adler32 algorithm | ✅ | For sector CRCs, verified with test vectors |
| Sector CRC validation | ✅ | ADLER32 checksums, single-unit and multi-sector |
| Sector CRC generation | ✅ | Generate CRCs when creating archives |
| CRC for encrypted files | ✅ | CRC table encryption support |

### File Flags

| Flag | Read | Write | Notes |
|------|:----:|:-----:|-------|
| FILE_EXISTS | ✅ | ✅ | File validity marker |
| FILE_COMPRESS | ✅ | ✅ | Multi-algorithm compression |
| FILE_SINGLE_UNIT | ✅ | ✅ | Non-sectored vs sectored files |
| FILE_ENCRYPTED | ✅ | ❌ | Decryption only |
| FILE_FIX_KEY | ✅ | ❌ | Key adjusted by block position |
| FILE_SECTOR_CRC | ✅ | ✅ | Per-sector checksums |
| FILE_PATCH_FILE | ✅ | ✅ | Patch file marker |
| FILE_DELETE_MARKER | ✅ | ✅ | Deletion markers in patches |
| FILE_IMPLODE | ❌ | ❌ | Legacy PKWARE, not implemented |

### Special Files

| File | Read | Write | Notes |
|------|:----:|:-----:|-------|
| (listfile) | ✅ | ✅ | File listing, auto-generated on write |
| (attributes) | ✅ | ✅ | CRC32, version 100 format |
| (signature) | ✅ | ❌ | Parse signatures (no crypto verification) |
| (patch_metadata) | ✅ | ❌ | MD5 hashes and base file size |

### Patch Chain Support

| Feature | Status | Notes |
|---------|:------:|-------|
| Multiple archives | ✅ | Priority-based file resolution |
| File overrides | ✅ | Higher priority archive wins |
| Deletion markers | ✅ | Mark files as deleted in patches |
| Patch file markers | ✅ | FILE_PATCH_FILE flag support |
| File location tracking | ✅ | Identify source archive for files |
| Unique file listing | ✅ | List files across all archives |
| Metadata reading | ✅ | Read (patch_metadata) from patches |

## Limitations

- **Audio files** (`.wav`) using Huffman+ADPCM compression are not supported
- **MPQ v3/v4** (Cataclysm+) are not supported
- **Signature verification** reads but does not cryptographically verify signatures
- **Listfile required** for file enumeration when reading archives
- **Encryption** can decrypt files but cannot encrypt when writing

All other game data files (DBC, BLP, M2, WMO, ADT, etc.) work correctly for both reading and writing.

## Compatibility

This library is **fully compatible** with MPQ archives created by:
- [StormLib](https://github.com/ladislav-zezula/StormLib) - Industry standard MPQ library
- [warcraft-rs](https://github.com/wowemulation-dev/warcraft-rs) - Rust MPQ implementation
- Official Blizzard clients and tools

Archives created by go-mpq can be read by all of the above. CRC32 and Adler32 implementations are verified against the same test vectors to ensure exact algorithmic compatibility.

## Test Coverage

The library includes comprehensive test coverage with 28 test suites covering:
- Core archive operations (create, read, extract, modify)
- Archive modification (add, remove, replace files)
- CRC32 and Adler32 algorithms (verified with test vectors)
- Sector CRC generation and validation
- Multi-sector file handling
- Patch chain operations with priorities
- Deletion markers and patch files
- Attributes file generation
- Encryption and compression
- V1 and V2 format compatibility
- Edge cases (empty archives, large files)

## License

MIT License - see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! Please feel free to submit issues and pull requests.

## Acknowledgments

- [StormLib](https://github.com/ladislav-zezula/StormLib) by Ladislav Zezula - Reference implementation
- [warcraft-rs](https://github.com/wowemulation-dev/warcraft-rs) by [contributors](https://github.com/wowemulation-dev/warcraft-rs/blob/main/CONTRIBUTORS.md) - Reference implementation.
- [The MoPaQ File Format](http://www.zezula.net/en/mpq/mpqformat.html) - Format specification
