# ADR 0001: Binary marker body with standard emulation prevention

**Type:** SNAPSHOT. Date: 2026-09-11. Status: accepted.

## Context

The marker travels as the payload of a `user_data_unregistered` SEI message. Any byte value can appear in it, and H.264 forbids the sequences `00 00 00`, `00 00 01`, `00 00 02` and `00 00 03` inside a NAL unit. Two designs avoid the problem: apply the standard emulation-prevention byte when writing and strip it when reading, or choose an encoding that never produces a zero byte.

## Decision

A fixed binary layout, big-endian, with the standard emulation-prevention byte applied over the whole SEI RBSP at write time and removed at read time.

## Consequences

- One mechanism, the one every H.264 reader must implement anyway. A zero-free encoding would add a second mechanism for no gain.
- The layout is readable in a hex dump and the codec is a few dozen lines in each language.
- Payload bytes are opaque to seimark; applications choose their own encoding.

## Alternatives rejected

- **Text payloads** such as `timestamp;counter`: avoid zero bytes by accident, waste space, and invite ad hoc parsing.
- **Varints or CBOR** for the fixed fields: smaller by a few bytes, less readable, more code.
- **Zero-free encoding**: avoids emulation prevention in the writer only; readers still need it for other SEI in the stream.
