# Contributing

## How work proceeds

A phase starts on its own branch. Design first: written to `docs/design/` and
approved before any code. Then a plan in `plans/`, with small tasks, each with
its test written first. Implementation follows the plan; a task that reveals a
decision produces an ADR, not a comment. Run the tests and linters and show
the output before claiming anything is done.

One pull request per phase, squash-merged, with Conventional Commits titles:
`feat:`, `fix:`, `docs:`, `ci:`, `chore:`, `refactor:`, `test:`.

## Running the checks

```sh
make test lint                       # Go: see the Makefile for each target
cd browser && npm ci && npm run typecheck && npm test && npm run build
npm run e2e -- --project=chromium    # Playwright loopback test
```

## Where documents live

`specs/` holds the constitution and ADRs, `docs/format.md` the wire format,
`docs/design/` one living design per component. `README.md` and
`browser/README.md` point at `docs/` rather than restating it. Full table and
rules in `CLAUDE.md`.

## Test vectors

Vectors in `vectors/` are never edited by hand to make a test pass. A failing
vector means the code or the specification is wrong — decide which, then fix
that.
