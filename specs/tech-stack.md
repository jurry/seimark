# Tech stack

**Type:** LIVING

## Approved

| Area | Choice | Why |
|---|---|---|
| Language, core | Go 1.26, pure Go | One static binary, cross-compiles everywhere, no CGO to keep `go get` painless. |
| MP4 and SEI primitives | `github.com/Eyevinn/mp4ff` (MIT, active) | Solid MP4 parsing including fragmented files, and SEI encode and decode with emulation-prevention handling. Building on it beats duplicating it (ADR 0004). |
| CLI | Standard library `flag` with subcommands | One binary, three subcommands; a framework would be most of the dependency tree. |
| Tests | Standard library `testing`, golden files in `vectors/` | The vectors are the format's conformance suite and are shared with the browser package. |
| Lint | golangci-lint v2, every linter on, each disabled one justified in .golangci.yaml; gofumpt and gci as formatters | Findings are read, not silenced; new linters are on until argued off. |
| Language, browser | TypeScript | Type checking across the codec and the transform; ships as an npm package. |
| Browser API | WebRTC Encoded Transform: `RTCRtpScriptTransform` in a worker, with `createEncodedStreams` as the fallback where the standard API is missing | Standard first. Measured 2026-09-13: Chromium 153 and Firefox 155 both expose the standard API, so the fallback covers only browsers older than those and has no coverage in the browser test matrix (ADR 0006, `docs/design/browser-package.md`). |
| Tests, browser package | Standard library `node:test` and `node:assert`, reading `../vectors/` | The tech stack forbids third-party assertion frameworks and the Go side uses standard library `testing` for the same reason; the vectors are shared, so both implementations are held to the same bytes. |
| Browser build | `esbuild` (MIT), development dependency only | The `RTCRtpScriptTransform` worker is created from a blob URL and a blob worker cannot import at runtime, so the compiled codec has to be emitted into the worker's source as a string constant (ADR 0006). Nothing ships in the package. |
| Browser end-to-end tests | `@playwright/test` (Apache-2.0), development dependency only | The only way to prove the real encoded-transform paths work against a real H.264 encoder. Chromium in CI; see the design for why Firefox is not. |
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
