# ADR 0002: One fixed UUID identifies the format; a version byte governs the body

**Type:** SNAPSHOT. Date: 2026-09-11. Status: accepted.

## Context

`user_data_unregistered` messages start with a 16-byte UUID that identifies who wrote them. Streams may carry such messages from encoders, cameras and other tools. Readers must never mistake foreign data for a marker, and the marker body must be able to change without breaking readers.

## Decision

- The UUID `44a73cb9-b36c-459a-8f1a-a3aa431f624a` identifies the seimark format. It never changes.
- The first body byte is the format version. Version 1 is defined in `docs/format.md`. A reader that meets a version it does not know returns an error rather than guessing.
- Reserved flag bits are written as zero and ignored on read, so minor additions do not require a new version.

## Consequences

- Foreign unregistered SEI is skipped silently; the reader reports only markers.
- Breaking changes bump the version byte; additive changes use reserved bits or the application payload.

## Alternatives rejected

- **Reusing the MISB ST 0604 UUID** so existing tools parse our time: rejected because the MISB body has no room for our fields. Compatibility is a separate message emitted alongside, see the roadmap.
- **Version in the UUID** (one UUID per version): makes readers search for several UUIDs and gains nothing over a byte.
