// Copyright (c) 2025 suprsokr
// SPDX-License-Identifier: MIT

/*
Package mpq provides pure Go support for reading and writing MPQ (Mo'PaQ) archives.

MPQ is an archive format created by Blizzard Entertainment, used in games like
Diablo, StarCraft, and World of Warcraft. This package supports MPQ format
versions 1 and 2, which covers games up through WoW: Wrath of the Lich King (3.3.5a).

# Features

  - Pure Go implementation - no CGO or external dependencies
  - Read and write MPQ archives
  - Support for MPQ format V1 (original, up to 4GB) and V2 (extended, >4GB)
  - Zlib compression support
  - Cross-platform compatibility

# Basic Usage

Creating an archive:

	archive, err := mpq.Create("patch.mpq", 100)
	if err != nil {
		log.Fatal(err)
	}
	defer archive.Close()

	err = archive.AddFile("local/file.txt", "Data\\file.txt")
	if err != nil {
		log.Fatal(err)
	}

Reading an archive:

	archive, err := mpq.Open("game.mpq")
	if err != nil {
		log.Fatal(err)
	}
	defer archive.Close()

	if archive.HasFile("Data\\file.txt") {
		err = archive.ExtractFile("Data\\file.txt", "output/file.txt")
		if err != nil {
			log.Fatal(err)
		}
	}

# Format Versions

Use [Create] for V1 format (compatible with all games) or [CreateV2] for
V2 format (required for archives >4GB, compatible with WoW: TBC and later).

# Path Conventions

MPQ archives use backslash (\) as the path separator. This package automatically
converts forward slashes to backslashes, so both formats work:

	archive.AddFile("src.txt", "Data\\SubDir\\file.txt")  // Native MPQ format
	archive.AddFile("src.txt", "Data/SubDir/file.txt")    // Also works

# Limitations

This package focuses on the subset of MPQ functionality needed for game modding:

  - No support for encrypted files (except hash/block table encryption)
  - No support for PKWare implode compression
  - No support for ADPCM audio compression
  - No support for MPQ format V3/V4 (Cataclysm+)
  - No support for patch archives
*/
package mpq
