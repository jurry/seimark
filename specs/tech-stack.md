# Tech stack

**Type:** LIVING

## Approved

| Area | Choice | Why |
|---|---|---|
| Language, core | Go 1.26, pure Go | One static binary, cross-compiles everywhere, no CGO to keep `go get` painless. |
| MP4 and SEI primitives | `github.com/Eyevinn/mp4ff` (MIT, active) | Solid MP4 parsing including fragmented files, and SEI encode and decode with emulation-prevention handling. Building on it beats duplicating it (ADR 0004). |
| CLI | Standard library `flag` with subcommands | One binary, three subcommands; a framework would be most of the dependency tree. |
| Tests | Standard library `testing`, golden files in `vectors/` | The vectors are the format's conformance suite and are shared with the browser package. |
| Language, browser | TypeScript | Type checking across the codec and the transform; ships as an npm package. |
| Browser API | WebRTC Encoded Transform: `RTCRtpScriptTransform` in a worker, with `createEncodedStreams` as the fallback where the standard API is missing | Standard first; the fallback covers older Chromium. Details decided in the phase 3 design. |
| Format identity | One fixed UUID, see `docs/format.md` | Readers ignore unregistered SEI with any other UUID, so foreign metadata never confuses them. |
| Licence | MIT | Maximum adoption for a small library; matches mp4ff. |

## No-fly list

Forbidden until an ADR supersedes the rule.

- **No CGO in the Go module.** No GStreamer, no FFmpeg bindings, no Tesseract. Anything that needs them lives in a separate module or an example.
- **No shelling out to ffmpeg** from the library or the CLI. Documentation may show ffmpeg commands for converting containers; the code does not run them.
- **No logging in the library.** Conditions are returned as values or errors; the CLI decides what to print.
- **No network code in the core.** Reading from WHEP, RTSP or HLS is glue that belongs to examples or sibling projects.
- **No third-party CLI, logging or assertion frameworks.**
- **No framework in the browser package** and no bundler-specific tricks: the worker is created from an inline blob so integration is one import.
- **No reuse of another format's UUID or layout.** The MISB ST 0604 compatibility mode emits the MISB message next to ours; it does not replace ours.
- **No codec other than H.264** without an ADR.
- **No comments that narrate the code.** See `CLAUDE.md`.
