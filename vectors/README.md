# Test vectors

The conformance suite for the seimark format. Every implementation of the
format, in any language, must produce these results.

- `markers/NNN-name.bin`: a marker body. `NNN-name.json`: the expected decode,
  or `{"error": "..."}` for bodies that must be rejected. Error codes:
  `unsupported_version`, `truncated`.
- `nal/NNN-name.bin`: a complete SEI NAL unit, emulation prevention applied.
  `NNN-name.json`: the markers a reader must find in it, as a JSON array.
- `streams/`: Annex B and MP4 fixtures with a marker before the first VCL NAL
  unit of every access unit, and the expected `seimark dump -out jsonl` output.
  `gen/` regenerates them; the results are committed.

Vectors are never edited to make a test pass.
