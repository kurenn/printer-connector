# Build Instructions

## Overview

This document describes how to build the printer-connector for different platforms using Docker for reproducible, cross-platform builds.

## Prerequisites

- **Docker** (required) - Ensures consistent builds across all platforms
- Go 1.22+ (optional, for local development only)

## Build Commands

**All production builds should use Docker** to ensure:
- Reproducible builds across different development machines
- Correct Go version (golang:1.23-alpine)
- Proper cross-compilation toolchain
- Smaller binaries (stripped with `-ldflags='-s -w'`)

### Local Development (macOS/Linux)

For quick local testing only (not for deployment):

```bash
go build -o printer-connector ./cmd/connector
```

### Raspberry Pi / Vanilla Klipper (ARM64)

**Recommended (Docker):**

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -c \
  "GOOS=linux GOARCH=arm64 go build -ldflags='-s -w' -o printer-connector-arm64 ./cmd/connector"
```

Expected size: ~5.4MB (stripped)

### K1 Max (MIPS Little-Endian)

**IMPORTANT:** K1 Max requires MIPS little-endian (`mipsle`), not big-endian (`mips`).

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -c \
  "GOOS=linux GOARCH=mipsle go build -ldflags='-s -w' -o printer-connector-mips ./cmd/connector"
```

Expected size: ~6.1MB (stripped)

### Alternative: MIPS Softfloat (if needed)

If the standard MIPS build doesn't work, try with softfloat:

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -c \
  "GOMIPS=softfloat GOOS=linux GOARCH=mipsle go build -ldflags='-s -w' -o printer-connector-mips ./cmd/connector"
```

## Architecture Notes

### Why Docker for All Builds?

Docker provides consistent, reproducible builds by ensuring:
1. **Same Go version** (1.23) across all development machines
2. **Proper cross-compilation** toolchain for each target architecture
3. **Static linking** with correct libc compatibility
4. **Stripped binaries** for smaller deployment size

The `-ldflags='-s -w'` flags:
- `-s` - Omit symbol table
- `-w` - Omit DWARF debug info
- Result: ~40% smaller binaries (9MB → 5-6MB)

### Binary Sizes (Stripped)

- **ARM64** (Raspberry Pi): ~5.4MB
- **MIPS** (K1 Max): ~6.1MB
- **Unstripped**: ~9.4MB (includes debug symbols)

## Verification

Check binary architecture and size:

```bash
# ARM64
ls -lh printer-connector-arm64
file printer-connector-arm64
# Should show: ELF 64-bit LSB executable, ARM aarch64, statically linked, stripped

# MIPS
ls -lh printer-connector-mips
file printer-connector-mips
# Should show: ELF 32-bit LSB executable, MIPS, MIPS32 version 1 (SYSV), statically linked, stripped
```

Note: **LSB** = Little-endian, **MSB** = Big-endian

## Troubleshooting

### "syntax error: unexpected '('" when running binary

This means the binary architecture doesn't match the CPU. Common causes:
- Using `mips` (big-endian) instead of `mipsle` (little-endian)
- Binary built with wrong Go version
- Binary corrupted during transfer

**Solution:** Rebuild with Docker using the command above.

### Binary works locally but fails on K1 Max

- Ensure you're using Docker build (not direct `go build`)
- Verify architecture with `file` command
- Check binary wasn't corrupted: `md5sum printer-connector-mips` before and after transfer

## Release Process

Build all variants using Docker for consistent, production-ready binaries:

```bash
# Create dist directory
mkdir -p dist

# ARM64 for Raspberry Pi
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -c \
  "GOOS=linux GOARCH=arm64 go build -ldflags='-s -w' -o dist/printer-connector-arm64 ./cmd/connector"

# MIPS for K1 Max
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -c \
  "GOOS=linux GOARCH=mipsle go build -ldflags='-s -w' -o dist/printer-connector-mips ./cmd/connector"

# Verify builds
ls -lh dist/
file dist/printer-connector-arm64
file dist/printer-connector-mips
```

Tag and upload to GitHub releases with both binaries.
