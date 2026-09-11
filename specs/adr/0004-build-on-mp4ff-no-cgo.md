# ADR 0004: Build on mp4ff; no CGO in the module

**Type:** SNAPSHOT. Date: 2026-09-11. Status: accepted.

## Context

The Go side needs MP4 parsing, including fragmented files, and SEI message encoding and decoding with emulation-prevention handling. Eyevinn's mp4ff provides both under the MIT licence and is actively maintained. GStreamer and FFmpeg bindings would give more, at the price of CGO, system libraries and a painful `go get`.

## Decision

- Use `github.com/Eyevinn/mp4ff` for MP4 sample iteration and for the generic SEI primitives. seimark owns only what mp4ff does not: access-unit walking in both byte-stream forms, marker placement, the marker codec, the stateful writer and the CLI.
- The module builds with `CGO_ENABLED=0`. Adapters that need GStreamer or FFmpeg live in separate modules or examples.

## Consequences

- One dependency, pure Go, cross-compiles to every platform Go supports.
- If mp4ff changes its SEI API, seimark follows; the surface used is small and pinned.

## Alternatives rejected

- **Own MP4 parser**: months of work to reach mp4ff's coverage of the box zoo.
- **FFmpeg or GStreamer bindings**: capability seimark does not need, dependencies its users would resent.
