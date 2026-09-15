# ADR 0008: Demultiplex MPEG-TS with go-astits

**Type:** SNAPSHOT. Date: 2026-09-16. Status: accepted.

## Context

Phase 4 adds a reader for MPEG-TS, the container MediaMTX records. Between the file and the access units the `h264` package already knows how to cut sit the parts of the transport stream that are not specific to this project: 188-byte packets with continuity counters and adaptation fields, PAT and PMT, payload reassembly, and PES headers with their 33-bit PTS and DTS. None of that is seimark's subject, and all of it is the part of MPEG-TS where a naive implementation is quietly wrong.

## Decision

- Use `github.com/asticode/go-astits` (MIT, pure Go) for demultiplexing: packets, PAT, PMT, reassembly and PES header parsing.
- Above it, seimark owns what is its own: choosing the first H.264 elementary stream, concatenating the PES payloads, cutting access units with the existing `h264.AccessUnits` rule, syncing from the first IDR NAL unit, and attaching the timestamps of the PES in which each access unit starts.
- The module stays pure Go with `CGO_ENABLED=0`, as ADR 0004 requires.

## Consequences

- A second dependency, with the same argument as ADR 0004 for mp4ff: the surface used is small, and reimplementing it would be work spent away from the format.
- The access-unit rule stays in one place, so MP4, Annex B and MPEG-TS cannot disagree about where an access unit begins.
- go-astits is a demultiplexer only. Writing MPEG-TS, if it is ever wanted, is a separate decision.

## Alternatives rejected

- **Hand-written demultiplexer**: FLV is a flat list of tags and is written by hand in this phase, but MPEG-TS is not FLV. Continuity counters, adaptation fields, sections spanning packets and PES reassembly are a few hundred lines that exist to be correct on damaged recordings, which is exactly the input this reader gets.
- **`github.com/bluenviron/mediacommon`**: covers MPEG-TS and FLV together, but brings a second MP4 implementation alongside mp4ff, and its H.264 handling cuts access units itself, which would give the project a second access-unit rule competing with `h264.AccessUnits`.
