# Build Instructions

## Overview

This document describes how to build the printer-connector for different platforms.

## Prerequisites

- Go 1.22+ or Docker (recommended for cross-compilation)
- For Docker builds: Docker installed and running

## Build Commands

### Local Development (macOS/Linux)

```bash
go build -o printer-connector ./cmd/connector
```

### Raspberry Pi / Vanilla Klipper (ARM64)

```bash
GOOS=linux GOARCH=arm64 go build -o printer-connector-arm64 ./cmd/connector
```

### K1 Max (MIPS Little-Endian)

**IMPORTANT:** K1 Max requires MIPS little-endian (`mipsle`), not big-endian (`mips`).

Use Docker for consistent cross-compilation:

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -c \
  "GOOS=linux GOARCH=mipsle go build -ldflags='-s -w' -o printer-connector-mips ./cmd/connector"
```

The `-ldflags='-s -w'` flags strip debug info and reduce binary size from ~9MB to ~6MB.

### Alternative: MIPS Softfloat (if needed)

If the standard MIPS build doesn't work, try with softfloat:

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -c \
  "GOMIPS=softfloat GOOS=linux GOARCH=mipsle go build -ldflags='-s -w' -o printer-connector-mips ./cmd/connector"
```

## Architecture Notes

### Why Docker for K1 Max?

The K1 Max uses a specific MIPS variant that requires:
1. **Little-endian** (`GOARCH=mipsle`) - not big-endian
2. Consistent Go toolchain version (1.23)
3. Static linking with proper libc compatibility

Using Docker ensures:
- Reproducible builds across different development machines
- Correct Go version (golang:1.23-alpine)
- Proper cross-compilation toolchain
- Smaller binaries (stripped)

### Binary Size

- **Unstripped:** ~9.4MB (includes debug symbols)
- **Stripped:** ~6.1MB (production ready)

## Verification

Check the binary architecture:

```bash
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

When creating a release, build all variants:

```bash
# ARM64 for Raspberry Pi
GOOS=linux GOARCH=arm64 go build -o dist/printer-connector-arm64 ./cmd/connector

# MIPS for K1 Max (use Docker)
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -c \
  "GOOS=linux GOARCH=mipsle go build -ldflags='-s -w' -o dist/printer-connector-mips ./cmd/connector"
```

Tag and upload to GitHub releases with both binaries.
