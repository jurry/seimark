# seimark marker format, version 1

**Type:** LIVING. Changes to this document that alter bytes on the wire require a new version number and an ADR.

## Overview

A marker is one H.264 SEI NAL unit carrying one `user_data_unregistered` message whose UUID identifies the seimark format. The message body holds the sender's wall-clock time, a sequence number, a stream identity and an optional application payload. Writers put exactly one marker into each access unit they stamp; readers return every marker they find.

## Container

| Field | Value |
|---|---|
| NAL unit header | `0x06`: `forbidden_zero_bit` 0, `nal_ref_idc` 0, `nal_unit_type` 6 (SEI) |
| `payloadType` | 5, `user_data_unregistered`, coded as one byte (values ≥ 255 use the standard run of `0xFF` bytes) |
| `payloadSize` | 16 + body length, coded the same way: each `0xFF` byte adds 255, the final byte adds its value |
| `uuid_iso_iec_11578` | `44 a7 3c b9 b3 6c 45 9a 8f 1a a3 aa 43 1f 62 4a`, the UUID `44a73cb9-b36c-459a-8f1a-a3aa431f624a` |
| body | as below |
| `rbsp_trailing_bits` | `0x80` |

The whole RBSP after the NAL header is subject to the standard emulation prevention: wherever two zero bytes are followed by a byte with value 0 to 3, a `0x03` byte is inserted before it on write and removed on read.

In Annex B streams the NAL unit is preceded by a start code; in MP4 and other length-prefixed forms by the sample's length field. The NAL unit bytes are identical in both.

## Body

Big-endian throughout. Fixed part 22 bytes.

| Offset | Size | Field | Meaning |
|---|---|---|---|
| 0 | 1 | version | `0x01`. Readers reject other values with an error. |
| 1 | 1 | flags | bit 0: time source, 0 = time of sending, 1 = time of capture. bit 1: application payload present. bits 2 to 7: reserved, written as 0, ignored on read. |
| 2 | 8 | origin time | Microseconds since the Unix epoch, UTC, signed 64-bit, on the sender's clock. |
| 10 | 4 | sequence | Unsigned 32-bit. Starts at 0 when a stream starts, increments by one per stamped access unit, wraps modulo 2^32. |
| 14 | 8 | stream id | Eight bytes chosen at random when a stream starts, or supplied by the application. |
| 22 | 2 | payload length | Present only when flags bit 1 is set. Unsigned 16-bit. |
| 24 | n | payload | Present only when flags bit 1 is set. Opaque bytes; the application decides their encoding. |

## Semantics

- **Origin time** is the sender's clock and nothing else. No synchronisation is implied. Consumers that compare senders' clocks estimate offsets themselves.
- **Time source** tells which moment the time describes. Capture time is preferred where the platform exposes it; time of sending is the fallback and is what a browser transform typically has.
- **Sequence** exists for gap and duplicate detection. Across a reconnect an application may keep the stream id and continue the sequence, or start a new stream id at 0. Readers see the difference: same id with a jump means loss, new id means a new stream.
- **Stream id** distinguishes streams that share a channel or a file. It is not a secret and not globally unique by guarantee; eight random bytes make collisions negligible in practice.
- **Payload** is limited by the 16-bit length. Writers should warn above 4096 bytes: large payloads cost bandwidth on every frame and are better sent on keyframes only.

## Placement

Writers insert the marker after any access-unit delimiter, parameter sets and existing SEI NAL units, and before the first VCL NAL unit (types 1 to 5) of the access unit. Appending after the last VCL NAL unit is a documented option for pipelines measured to need it. Readers accept either.

## Compatibility with MISB ST 0604

Optional and separate: a writer may add the MISB precision time stamp as its own `user_data_unregistered` message with the MISB UUID, carrying the same origin time. Tools that know MISB then read the time; tools that know seimark read everything. The two messages never share a UUID or a body.

## Worked example

A marker with version 1, flags 0 (time of sending, no payload), origin time 2026-09-11T21:00:00Z, sequence 0 and stream id `9f3c1a77e2b04d51`.

Origin time: 1789160400000000 microseconds, `00 06 5b 3b 5e 16 94 00`.

Body, 22 bytes:

```
01 00 00 06 5b 3b 5e 16 94 00 00 00 00 00 9f 3c 1a 77 e2 b0 4d 51
```

SEI RBSP before emulation prevention, 41 bytes: type 5, size 38 (`0x26`), UUID, body, trailing bits:

```
05 26 44 a7 3c b9 b3 6c 45 9a 8f 1a a3 aa 43 1f 62 4a
01 00 00 06 5b 3b 5e 16 94 00 00 00 00 00 9f 3c 1a 77 e2 b0 4d 51 80
```

The zero run across the end of the time and the sequence needs two emulation-prevention bytes. NAL unit as written, 44 bytes:

```
06 05 26 44 a7 3c b9 b3 6c 45 9a 8f 1a a3 aa 43 1f 62 4a
01 00 00 06 5b 3b 5e 16 94 00 00 03 00 00 03 00 9f 3c 1a 77 e2 b0 4d 51 80
```

This example is the first entry in `vectors/`.
