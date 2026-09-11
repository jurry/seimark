# seimark: instructions for coding agents

Read this before inspecting or editing anything. `AGENTS.md` points here.

## What this is

Per-frame metadata in H.264 SEI: a marker format, a Go library and CLI, a browser library. The mission, approved technology and roadmap are the constitution in `specs/`; read `specs/mission.md`, `specs/tech-stack.md` and `specs/roadmap.md` first. The no-fly list in the tech stack is binding.

## Documents and where they live

| What | Where | Rule |
|---|---|---|
| Constitution | `specs/mission.md`, `specs/tech-stack.md`, `specs/roadmap.md` | LIVING. Changed only between phases, in a commit of their own. |
| Decisions | `specs/adr/NNNN-title.md` | SNAPSHOT. Never edited; superseded by a new ADR that names the old one. |
| Format specification | `docs/format.md` | LIVING. Wire changes need a version bump and an ADR. |
| Designs | `docs/design/<component>.md` | LIVING. One per component, rewritten to current truth when the component changes. No dated duplicates. |
| Implementation plans | `plans/` | Not versioned. Written before a phase, deleted when it ships. What must survive goes into a design, an ADR or the roadmap. |
| Test vectors | `vectors/` | Versioned. The conformance suite for every implementation of the format. |

Every document states its type, LIVING or SNAPSHOT, under its title. No "Update:" notes stacked at the bottom of a living document; edit the sentence, git holds the history.

## How work proceeds

1. A phase starts on its own branch, named after the phase.
2. Design first: the design for the phase is written to `docs/design/` and approved before any code.
3. Then a plan in `plans/`, with small tasks, each with its test written first.
4. Implementation follows the plan. A task that reveals a decision produces an ADR, not a comment.
5. Verification before any claim of done: run the tests and the linters and show the output.
6. When the phase ships, the roadmap moves the marker, the design is rewritten to what was built, the plan is deleted.

## Code rules

- Go: `gofmt`, `go vet`, tests in the standard library style, table-driven where there are several cases. `CGO_ENABLED=0` must build.
- Errors carry context and are returned, never logged, from library code.
- Public API is documented with a sentence that says what a reader could not infer from the signature.
- Comments are the exception. Explain a non-obvious invariant, a workaround, or why not the obvious alternative. Never narrate what the code does. One line where possible. Simple English.
- Vectors are never edited by hand to make a test pass. A failing vector means the code or the specification is wrong; decide which, then fix that.

## Git

- Never commit or push without explicit approval for the specific change. Show the diff, then wait.
- Approval of a design or a plan is not approval to commit code.
- One logical change per commit, with a message that says why.
