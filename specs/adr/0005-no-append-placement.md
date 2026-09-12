# ADR 0005: One placement; the reader follows the standard access-unit rule

**Type:** SNAPSHOT. Date: 2026-09-12. Status: accepted. Supersedes the append option of ADR 0003.

## Context

ADR 0003 kept appending the marker after the last VCL NAL unit as an option, and phase 1 gave the Annex B reader an exception so appended markers could be read back: a marker SEI joins the access unit it follows when that unit carries no marker yet.

The exception is ambiguous. In a keyframes-only stream the bytes `[P][SEI][IDR]` are what both placements produce: the appended marker of the P picture, and the prepended marker of the IDR picture. The reader cannot tell them apart, because the P unit carries no marker of its own in either case, and it attributes the marker to the wrong picture in one of them. The ambiguity is not hypothetical: `-keyframes-only` with the documented before-VCL placement writes exactly that shape.

No pipeline has been measured to need append placement. The option was speculative.

## Decision

- The writer has one placement: after any access-unit delimiter, parameter sets and existing SEI NAL units, and before the first VCL NAL unit. An access unit with no VCL NAL unit is `ErrNoVCL`.
- The reader follows H.264's access-unit rule with no exception: once the current access unit holds a VCL NAL unit, a delimiter, SPS, PPS or SEI, or a VCL NAL unit with `first_mb_in_slice == 0`, starts the next one.

## Consequences

- Keyframes-only streams read back correctly: every marker precedes its own picture, so there is nothing to guess.
- The writer and the reader are simpler; `WriterOptions` loses a field and `inject` loses a flag.
- A pipeline that needs append placement has to show the measurement first. That reopens this ADR and needs an answer to the ambiguity above, not just an option.

## Alternatives rejected

- **Stream-level detection from the first marker**: decide the placement of the whole stream from where the first marker sits relative to the first picture, then apply it everywhere. It fails on a stream cut to start at a non-IDR picture, where the first unit seen carries no marker and the first marker seen belongs to the picture that follows it; the detector reads that as append placement and misattributes every marker in the file.
