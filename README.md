# go-mpq

[![Go Reference](https://pkg.go.dev/badge/github.com/suprsokr/go-mpq.svg)](https://pkg.go.dev/github.com/suprsokr/go-mpq)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Pure Go library for reading and writing MPQ (Mo'PaQ) archives.

MPQ is an archive format created by Blizzard Entertainment, used in games like Diablo, StarCraft, and World of Warcraft. This package supports MPQ format versions 1 and 2, covering games up through WoW: Wrath of the Lich King (3.3.5a).

## Features

- **Pure Go** - No CGO, no external dependencies
- **Cross-platform** - Works on Windows, macOS, and Linux
- **Read & Write** - Create new archives and extract from existing ones
- **V1 & V2 Support** - Original format and Burning Crusade extended format
- **Zlib Compression** - Automatic compression for smaller archives

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

### Using V2 Format

For archives that may exceed 4GB or for better compatibility with WoW: TBC and later:

```go
// Create V2 format archive
archive, err := mpq.CreateV2("large-patch.mpq", 1000)
```

## API Reference

### Functions

| Function | Description |
|----------|-------------|
| `Create(path, maxFiles)` | Create new V1 format archive |
| `CreateV2(path, maxFiles)` | Create new V2 format archive |
| `CreateWithVersion(path, maxFiles, version)` | Create archive with specific version |
| `Open(path)` | Open existing archive for reading |

### Archive Methods

| Method | Description |
|--------|-------------|
| `AddFile(srcPath, mpqPath)` | Add file to archive (write mode) |
| `ExtractFile(mpqPath, destPath)` | Extract file from archive (read mode) |
| `HasFile(mpqPath)` | Check if file exists in archive |
| `Close()` | Close archive (writes if in write mode) |

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

This library focuses on MPQ v1 and v2 formats used by World of Warcraft through Wrath of the Lich King (3.3.5a).

### Archive Operations

| Operation | Read | Write | Notes |
|-----------|:----:|:-----:|-------|
| Open/Create V1 archive | ✅ | ✅ | Original format, up to 4GB |
| Open/Create V2 archive | ✅ | ✅ | Extended format, >4GB support |
| Add files | - | ✅ | |
| Extract files | ✅ | - | |
| List files (listfile) | ✅ | ✅ | Auto-generated on write |
| Delete files | ❌ | ❌ | Mark as deleted |
| Compact archive | ❌ | ❌ | Reclaim deleted space |
| Modify existing archive | ❌ | ❌ | Currently create-only |

### Compression Support

| Method | Read | Write | Notes |
|--------|:----:|:-----:|-------|
| Zlib (0x02) | ✅ | ✅ | Most common in WoW |
| PKWare DCL (0x08) | ✅ | ❌ | Older/base MPQ files |
| BZip2 (0x10) | ✅ | ❌ | Some WC3+ files |
| Huffman (0x01) | ❌ | ❌ | Audio files only |
| ADPCM Mono (0x40) | ❌ | ❌ | Audio files only |
| ADPCM Stereo (0x80) | ❌ | ❌ | Audio files only |
| Multi-compression | ✅ | ❌ | Chained algorithms |

### File Flags

| Flag | Read | Write | Notes |
|------|:----:|:-----:|-------|
| FILE_EXISTS | ✅ | ✅ | |
| FILE_COMPRESS | ✅ | ✅ | |
| FILE_SINGLE_UNIT | ✅ | ✅ | |
| FILE_ENCRYPTED | ✅ | ❌ | Decryption only |
| FILE_KEY_V2 (FIX_KEY) | ✅ | ❌ | Adjusted key decryption |
| FILE_IMPLODE | ❌ | ❌ | Legacy compression flag |
| FILE_SECTOR_CRC | ❌ | ❌ | Sector checksums |
| FILE_PATCH_FILE | ❌ | ❌ | Patch archives |
| FILE_DELETE_MARKER | ❌ | ❌ | Deletion markers |

### Special Files

| File | Read | Write | Notes |
|------|:----:|:-----:|-------|
| (listfile) | ✅ | ✅ | File listing |
| (attributes) | ❌ | ❌ | CRC32, timestamps, MD5 |
| (signature) | ❌ | ❌ | Archive signatures |

## Limitations

- **Audio files** (`.wav`) using Huffman+ADPCM compression are not supported
- **MPQ v3/v4** (Cataclysm+) are not supported
- **Patch archives** with incremental updates are not supported
- **Write mode** is create-only; modifying existing archives is not yet supported

All other game data files (DBC, BLP, M2, WMO, ADT, etc.) should work correctly for both reading and writing.

## License

MIT License - see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! Please feel free to submit issues and pull requests.

## Acknowledgments

- [StormLib](https://github.com/ladislav-zezula/StormLib) by Ladislav Zezula - Reference implementation
- [The MoPaQ File Format](http://www.zezula.net/en/mpq/mpqformat.html) - Format specification
