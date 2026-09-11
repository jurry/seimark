# ADR 0003: The marker goes before the first VCL NAL unit of the access unit

**Type:** SNAPSHOT. Date: 2026-09-11. Status: accepted.

## Context

A SEI NAL unit can be placed anywhere before the first VCL NAL unit of an access unit. Appending it after the last VCL NAL unit is easier to implement, because nothing has to be parsed, but H.264 treats a SEI that follows the last VCL NAL unit as the start of the next access unit. Some pipelines keep the whole access unit together and tolerate it; standard-conforming parsers may attribute the marker to the wrong frame or drop it at the end of a stream.

## Decision

The writer inserts the marker after any access-unit delimiter, SPS, PPS and existing SEI, and before the first VCL NAL unit. Appending at the end of the access unit is available as an explicit option for pipelines that have been measured to require it.

## Consequences

- The writer must find NAL unit boundaries in the access unit it stamps. That code exists anyway for the reader.
- The reader accepts a marker anywhere in the access unit, so both placements decode.
- Which placement survives which media server is measured by the sibling passthrough project, not assumed here.

## Alternatives rejected

- **Append only**: simplest writer, wrong per the standard, and would tie seimark to pipelines that happen to tolerate it.
