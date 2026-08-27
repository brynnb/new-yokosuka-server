# Script authoring and runtime

The server contains a versioned community script repository and a live
server-authoritative Yarn event runtime. Narrative design, canon, client editor
layout, and scene presentation are outside this repository; this document
covers storage, compilation, review, publication, preview, and execution.

## Optional compiler boundary

`tools/yarn-compiler` is a .NET 9 process using the pinned official
`YarnSpinner.Compiler` package. The Go server communicates with it over bounded
JSON messages and receives diagnostics, compiled program bytes, string data,
and node metadata.

Set `YARN_COMPILER_PATH` to the published executable to enable:

- compilation during draft saves and publication workflows;
- script previews and test fixtures; and
- server-authoritative scripted events over WebSocket.

Without that variable, account, world, NPC, chat, economy, and other server
features remain available. Script repository reads remain available, but
compiler-dependent authoring and runtime operations fail explicitly rather than
using a fallback parser.

## Repository model

The PostgreSQL schema stores scripts, immutable numbered versions, compiled
artifacts, triggers, dependencies, identifier usage, collaborators, review
threads and comments, moderation events, test fixtures, and native-source
provenance.

New scripts use `contentFormat: "yarn"`. A typical lifecycle is:

1. a registered account creates a script and initial draft;
2. an authorized owner or collaborator saves source and trigger changes;
3. a new immutable version is created from a draft;
4. the version is submitted and reviewed;
5. an authorized publisher publishes it; and
6. rollback selects an earlier eligible version by creating the appropriate
   audited repository transition.

Authorization and valid status transitions are enforced in `internal/store`,
not solely in an editor. Guest accounts cannot author scripts. Published
versions cannot be edited in place. Archival is soft and audited.

The complete HTTP dispatch lives under `/api/scripts`; route parsers and tests
in `internal/httpapi` are the authoritative endpoint reference. `GET
/api/script-schema` returns the current command registry plus identifiers found
in published scripts.

## Commands and triggers

`internal/scriptcontent/commands.v1.json` is the embedded command registry.
Compilation receives the same registry used by the server so function names,
parameter types, dependency rules, and identifier kinds are checked against one
definition.

Triggers can select an event by kind and optional actor, object, activity, or
area constraints. Publication rejects ambiguous triggers at the same priority.
At runtime the engine resolves a published trigger for the requesting character
and current world context.

## Live execution

When the compiler bridge is enabled, WebSocket clients use
`script_event_start` and `script_event_advance`. The server responds with
`script_event_yield` or `script_event_rejected`.

Each character may have at most one active script event. The server owns the
run lease, selected script/version, current node, variables, choices, effects,
trace, and per-character story state. Supported advance actions are
`continue`, `select`, and `cancel`; option selection includes an option ID.

Client presentation begins only after a server yield. The client may render a
line, choices, movement, or other declared events, but it does not decide
durable effects or story progression.

## Built-in scripts

`internal/officialscript` contains reviewed built-in script sources and import
metadata. With PostgreSQL and the compiler configured, import them through:

```sh
go run ./cmd/script-import -builtin all
```

Pass one built-in name instead of `all` to import a single reviewed script. The
same command also accepts JSON batches in `recovered-reference` and
`official-yarn` formats. Built-in source provenance is retained in the
script-version schema; generated or recovered content should not be presented
as independently authored source.

## Preview and testing

Preview runs use explicit fixtures and are bounded to 512 emitted events. They
execute through the same compiler/runtime bridge but do not replace live
authorization and persistence tests.

Ordinary unit tests run with:

```sh
go test ./...
```

Compiler-backed tests additionally require:

```sh
export NEW_YOKOSUKA_YARN_COMPILER="$PWD/.build/yarn-compiler/NewYokosuka.YarnCompiler"
go test ./...
```

PostgreSQL script integration tests also require
`NEW_YOKOSUKA_TEST_DATABASE_URL`. CI publishes the compiler before running the
compiler-backed suite.
