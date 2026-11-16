# agy-import

Create a new Antigravity CLI conversation from selected user and assistant history, without running a model turn during import.

**Status: Go client implemented; native importer pending.** The independent [client module](client/) provides typed imports and previews, executable discovery, stable operation IDs, and subprocess handling, with contract tests against a fake helper. This repository does not yet provide the native importer executable, a released client tag, or a verified Antigravity storage adapter. The current supported-native-build set is empty.

## Make consumption easy

`showagent` should integrate through one typed Go call:

```go
result, err := importer.Import(ctx, request)
```

The client resolves the executable, fills the protocol envelope and operation ID, sends the request on stdin, and validates a structured result. The planned helper checks target compatibility, reconciles retries, creates the session, and verifies its output inside that operation. Consumers do not need to sequence `doctor`, `capabilities`, or `status` commands.

The client is an independent Go module. The native helper will have a separate module and release lifecycle:

| Module | Responsibility |
| --- | --- |
| `github.com/drolevar/agy-import` | CLI, native Antigravity adapters, installation and recovery |
| `github.com/drolevar/agy-import/client` | Public request/result types and subprocess client; standard library only |

The separate client module keeps SQLite and protobuf dependencies out of `showagent`'s module graph. The process boundary remains available to consumers in any language.

See the [client README](client/README.md) for the implemented API, discovery order, retry semantics, and development commands. Its process tests cover synthetic helper behavior; they do not establish native import compatibility.

## Scope of the first implementation

- Create new conversations from ordered user/assistant text already selected by the caller.
- Associate each conversation with an explicitly chosen local workspace.
- Provide a native conversation database, the required metadata, and the transcript mirror needed by `showagent` discovery.
- Make repeated delivery of the same operation recoverable and idempotent.
- Return the conversation ID, workspace, separate database/transcript paths, warnings, and accurate verification results.
- Keep source parsing, scope selection, UI, and subsequent resume in `showagent`.

The first implementation does not transfer native execution state, pending tools, attachments, permission grants, or model internals. A selected transcript is historical context. Import itself must not execute instructions in it or initiate a model request.

## Documents

| Document | Purpose |
| --- | --- |
| [Architecture](docs/architecture.md) | Why the helper exists, ownership, target storage and recovery |
| [Showagent integration](docs/showagent-integration.md) | One-call consumer API, discovery, packaging and changes to the existing adapter |
| [Protocol v1](docs/protocol.md) | Request/result contract, operation identity and error semantics |
| [Implementation plan](docs/implementation-plan.md) | Current progress, remaining milestones, and evidence needed before enabling native writes |
| [Request schema](schema/import-request-v1.schema.json) | Machine-readable request shape |
| [Response schema](schema/import-response-v1.schema.json) | Success, preview and error shapes |

Examples are in [examples/](examples/). The protocol is a proposed v1 contract and may still change before its first release. Once released, its documented compatibility rules apply.

## Planned command interface

These commands will become available when the CLI is implemented:

```sh
agy-import import --input conversation.json --json
agy-import import --input - --json
agy-import import --input conversation.json --dry-run --json
agy-import capabilities --json
agy-import doctor --json
agy-import status --operation-id OPERATION_ID --json
```

The normal integration needs only `import`. `--dry-run` is the preview operation; the last three commands are optional diagnostics. The default target is the Antigravity CLI store. An explicit `--store` override will support isolated tests and alternate profiles; it refers directly to the directory containing `conversations/`, not to the parent directory containing multiple Antigravity surfaces.

## Distribution requirement

Publish one executable per supported OS/architecture, stable archive names, checksums, and a compatibility manifest. The client can find a helper installed beside the calling executable or on `PATH`, with explicit configuration taking precedence. A future `showagent` distribution can bundle a pinned helper as a second executable. Imports must not trigger downloads or updates.

No published installation command or release asset is available yet. The client module can be built and tested from this checkout; native imports require the remaining implementation and compatibility gates.

## Prior work

[`session-migrate`](https://github.com/xhluca/session-migrate/blob/45351ff95ce79684c23377920b92026ecbe9759b/docs/antigravity-format.md) provides useful independent research and native test evidence for exactly `agy 1.1.16` on Linux x86-64. Its evidence is a starting reference, not a compatibility claim for this project or later Antigravity releases. See [the implementation plan](docs/implementation-plan.md) for how that knowledge will be revalidated.

## License

[MIT](LICENSE).
