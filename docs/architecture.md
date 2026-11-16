# Architecture

This document describes the full architecture. The independent [Go client](../client/) is implemented and tested against a fake helper. The CLI, native Antigravity writer, and recovery installer remain planned; no installed Antigravity build is currently claimed as supported.

## Purpose

`agy-import` will create a new Antigravity CLI conversation from an ordered user/assistant transcript without starting a model turn. `showagent` will remain responsible for finding, reading, and selecting the source conversation. The importer will own Antigravity persistence and recovery.

Ease of consumption is a design requirement. A normal Go consumer should construct a request and call `client.Import(ctx, request)` once. It should not sequence capability checks, diagnostics, staging, installation, and status polling. The helper performs the required checks as part of the import; diagnostic commands remain optional tools for people and troubleshooting.

The public JSON process contract and a small Go client make the same operation available to both Go and non-Go callers. See [the protocol](protocol.md) and [showagent integration](showagent-integration.md).

## Evidence and scope

The inspected showagent revision is [`6a2756ac1556fdbb8ca8932c249bc57558287e7b`](https://github.com/drolevar/showagent/tree/6a2756ac1556fdbb8ca8932c249bc57558287e7b). Its [conversion path](https://github.com/drolevar/showagent/blob/6a2756ac1556fdbb8ca8932c249bc57558287e7b/internal/session/handoff.go) passes selected `Turn` values containing only `Role` and `Text` to a provider writer. Its [recorded Antigravity tests](https://github.com/drolevar/showagent/blob/6a2756ac1556fdbb8ca8932c249bc57558287e7b/docs/compatibility.md#local-antigravity-cli-build-2026-10-16) explain why creation through a model turn was withheld.

Independent [session-migrate format documentation](https://github.com/xhluca/session-migrate/blob/45351ff95ce79684c23377920b92026ecbe9759b/docs/antigravity-format.md) establishes useful prior art for generated SQLite/protobuf sessions, pinned to Antigravity CLI 1.1.16 on Linux x86-64. Its [native test](https://github.com/xhluca/session-migrate/blob/45351ff95ce79684c23377920b92026ecbe9759b/tests/test_antigravity_native.py) checks history replay to a local provider and persistence of a new turn. These are reference findings, not tests run by this project or evidence for later Antigravity builds.

Version 1 will transfer selected user/assistant text into a new conversation. It will not claim to transfer tool execution state, attachments, permission grants, subagents, or the original agent's runtime state. The importer preserves the text it receives; it cannot recover content that source extraction has already omitted. Resume remains a separate caller-controlled action.

## Responsibilities

| Concern | Owner |
| --- | --- |
| Source discovery, source parsing, scope selection | showagent |
| User-facing preview and destination workspace selection | showagent |
| Typed request/result objects and child-process transport | Public Go client |
| Target build/schema checks and request validation | agy-import helper |
| Native IDs, protobuf encoding, SQLite records, required mirrors | Antigravity adapter |
| Publication, idempotency, recovery, receipts | Import installer |
| Picker integration and optional resume | showagent |

Source workspace paths are provenance. The destination workspace must be an explicit local path or a caller-resolved local default. The helper validates it and converts it into any native URI representation. It does not rewrite paths appearing in message text or copy project files.

## Go modules and distribution

The production helper will be Go. The public client is a separate standard-library-only module, `github.com/drolevar/agy-import/client`, intended to have its own release tags. This prevents the helper's SQLite/protobuf requirements from entering showagent's module dependency graph.

The client handles executable discovery, request encoding, operation identity, bounded stdout/stderr capture, cancellation, result decoding, and typed errors. Native compatibility checks and storage decisions belong in the helper. A supported import will not require a preliminary `doctor` or `capabilities` invocation.

Helper discovery must support an explicit executable override, an installation beside showagent, and `PATH`, in a documented order. Release packaging must make at least one of these paths straightforward. The client must not download or update an executable during an import. It invokes an argument vector directly, without a shell.

The helper is expected to use `database/sql` with [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite), and the [official Go protobuf packages](https://pkg.go.dev/google.golang.org/protobuf). Low-level wire handling must remain inside a documented adapter. Dependency versions will be selected and pinned when implementation begins.

## Native adapters and discovery

A storage adapter is selected by verified target-build and schema evidence. Unknown combinations fail before publication. A matching SQL table layout alone is insufficient proof that protobuf semantics and auxiliary files are compatible.

Current showagent [discovers Antigravity through JSONL mirrors](https://github.com/drolevar/showagent/blob/6a2756ac1556fdbb8ca8932c249bc57558287e7b/internal/session/antigravity.go), checks separately for the conversation database, and reads workspace metadata from protobuf/cache sources. Consequently, an integration-ready import must cover the conversation database, required summary/workspace metadata, and a verified transcript mirror. The result distinguishes database and transcript paths; showagent's `Row.File` receives the transcript path.

Native metadata must be handled as protobuf binary data. [Protobuf documentation](https://protobuf.dev/programming-guides/proto3/#unknown-fields) explains that JSON conversion can lose unknown fields. Preserving unknown bytes does not establish that a modification is semantically valid; shared metadata updates still require native tests. New conversations should be built from understood records rather than copied from a populated user session.

## Installation and recovery

The importer validates the request and target, establishes destination-profile quiescence, and records operation intent before publishing artifacts. It then builds the new conversation in staging, validates native records, backs up shared metadata that will change, publishes required files, updates indexes, reads back the result, and commits its receipt.

Backups use [SQLite-aware snapshot mechanisms](https://sqlite.org/backup.html). Copying only a live database file can miss WAL contents. An available SQLite write lock does not establish application-level quiescence: another process may retain metadata in memory.

The conversation database, summary store, mirrors, and receipt cannot be treated as one filesystem-wide SQLite transaction. [SQLite documents](https://sqlite.org/wal.html) that even attached WAL databases are atomic individually rather than collectively. A durable operation journal must therefore support recovery after every publication boundary. Cleanup is limited to artifacts owned by that operation, with identity checks; it must never remove an imported conversation that has subsequently advanced.

The wire request requires `operation_id`. The Go client accepts an existing caller identity or derives `sha256:<digest>` from the canonical semantic request defined by the protocol. It retains this identity on errors, so even a process killed before stdout has a known operation ID. The helper independently checks the payload digest for conflicts and namespaces its ledger by the canonical destination store. Reusing an ID with different content is a conflict. An exact retry reconciles the previous attempt and returns the original target identity. A timeout must not cause a retry to allocate a new conversation automatically.

Errors distinguish an invocation with no published native changes, a known commit followed by a later failure, and an uncertain outcome. A no-change result for the current invocation does not rule out a commit by an earlier attempt under the same operation ID. Private staging or intent records may exist before publication. Cancellation cannot guarantee rollback after publication. The client does not retry mutations automatically. Repeating the same request is the normal recovery entry point; callers should not need to orchestrate storage repair.

## Repository layout and remaining components

| Path | Purpose |
| --- | --- |
| `docs/` | Architecture, protocol, integration, compatibility evidence |
| `schema/`, `examples/` | Public schemas and synthetic contract examples |
| `client/` | Independent Go client module and protocol DTOs |
| `cmd/agy-import/` (planned) | CLI dispatch and process I/O |
| `internal/antigravity/` (planned) | Target detection and native adapters |
| `internal/install/` (planned) | Publication, operation journal, recovery |
| `testdata/` (planned) | Native fixtures and compatibility evidence |

The client's synthetic fixtures live in `client/testdata/` so its module tests are self-contained. The [implementation plan](implementation-plan.md) defines the remaining gates to a usable native release.
