# Printer Connector: Claude Coding Guide

## Project
- **Repository**: `printer-connector`
- **Language**: Go
- **Purpose**: Runs on Klipper printers and executes commands from `print_dock` based on `print-contracts`.

## Default Workflow
1. Read contract changes first in `../print-contracts/openapi/connector-api.yaml` plus related `schemas/` and `fixtures/`.
2. Implement connector behavior in small, safe changes with strict input validation.
3. Preserve command lifecycle behavior and completion semantics.
4. Add/update Go tests for happy path, validation failures, and error propagation.
5. Run:
   - `scripts/contract_check`
   - `go test ./...`

## Safety Requirements
- Keep network and file operations bounded (timeouts, max-size limits).
- Avoid unsafe filename handling and path traversal issues.
- Do not log secrets, tokens, or sensitive payloads.
- Prefer retry-safe behavior and explicit errors.

## Skills To Use With Claude
- [go-patterns-best-practices](https://mcpmarket.com/tools/skills/go-patterns-best-practices)
- [go-development-patterns-3](https://mcpmarket.com/tools/skills/go-development-patterns-3)
- [go-testing-patterns-zh-tw](https://mcpmarket.com/tools/skills/go-testing-patterns-zh-tw)

## Output Expectations For Feature Work
When implementing a feature, provide:
1. Implementation summary
2. File-by-file change list
3. Validation commands executed and outcomes
4. Compatibility assumptions and residual risks
