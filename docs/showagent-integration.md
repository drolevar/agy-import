# Easy consumption by showagent

This is the integration contract for the implemented [Go client](../client/). The API below is available in this checkout and is tested against a fake helper. The native importer, release tags, and showagent adapter are still pending; a real import requires the native implementation and compatibility gates.

## Consumer acceptance criterion

A normal import is one `Import(ctx, request)` call. `showagent` supplies its selected messages, source identity and local destination workspace. It consumes a typed result or error. It must not implement Antigravity schema detection, protobuf encoding, backup policy, staging, index maintenance or recovery ordering.

Use the existing conversion seam rather than the public `showagent transcript --json` command. At [showagent commit 6a2756a](https://github.com/drolevar/showagent/blob/6a2756ac1556fdbb8ca8932c249bc57558287e7b/internal/session/handoff.go), `Convert` extracts `[]Turn`, applies the selected scope and calls `WriteConverted`. The public transcript command is intentionally redacted, defaults to 50 turns and caps output at 500; it cannot transparently carry every conversion.

## Public Go API

```go
import agyimport "github.com/drolevar/agy-import/client"

importer := agyimport.New(agyimport.Options{
    Executable: configuredHelperPath, // Empty enables default discovery.
    Store:      configuredStore,      // Empty selects the default CLI store.
})

result, err := importer.Import(ctx, agyimport.Request{
    OperationID: operationID, // Reuse an existing import-plan ID when available.
    Source: agyimport.Source{
        Application:    "showagent",
        Provider:       string(source.Provider),
        ConversationID: source.ID,
        Workspace:      source.CWD,
        Selection:      selectedScope,
    },
    Workspace: targetWorkspace,
    Title:     title,
    Messages:  messages, // []agyimport.Message, each with Role and Text.
})
```

`New` only stores configuration; it does not launch a process. `Import` accepts an existing operation ID. For a valid request with no ID, the client derives the protocol-defined stable ID before starting the helper and retains it on transport errors. This makes retrying the same request useful even if the process dies before emitting stdout. The client fills protocol/version/representation constants and the unredacted, untruncated defaults; callers do not construct a wire envelope.

`Request` also exposes explicit `Truncated`, `Redacted` and `AllowTruncatedHistory` fields. A deliberate selection of the last N turns is a selection, not unintended truncation. Importing incomplete producer output requires explicit acceptance. Optional source fields do not have to be fabricated when a source reader lacks them.

`Preview(ctx, request)` uses the same contract and `import --dry-run`, returning a typed plan under the helper's no-write contract. `RequestDigest(request)` exposes the semantic digest without launching a process. A `Probe` API is not implemented; a future diagnostic command may support a settings screen, but a prior probe is not required for `Import`, and a cached successful probe never substitutes for the helper's per-import checks.

## Typed results and errors

The result exposes `OperationID`, `ConversationID`, `Workspace`, `TranscriptPath`, `DatabasePath`, `MessagesWritten`, `Status`, `Warnings` and `Verification`. Returned paths are absolute paths in the destination environment. A success is accepted only if its operation ID matches the request, mandatory fields are present, message counts agree and the protocol version is understood.

`TranscriptPath` becomes `Row.File`. `DatabasePath` is a distinct field: the existing [Antigravity reader](https://github.com/drolevar/showagent/blob/6a2756ac1556fdbb8ca8932c249bc57558287e7b/internal/session/antigravity.go) parses `Row.File` as JSONL. Returning the SQLite path there breaks transcript reads and receipt verification.

The client exposes a typed error with `Code`, `Outcome`, `OperationID`, `Message`, and an underlying cause. `errors.Is(err, context.Canceled)` and `errors.Is(err, context.DeadlineExceeded)` remain useful. Codes are stable strings; unknown codes still produce a useful generic error. Callers must not parse stderr text or exit-code wording to determine the outcome.

Outcomes distinguish `not_started`, `committed` and `uncertain` for the current invocation. A failure before process creation is `not_started`; this says nothing about a previous attempt under the same operation ID. Once the helper has started, cancellation, malformed stdout or an unexplained exit may be `uncertain`. The client never claims that cancellation rolled back a published import or erases earlier uncertain/committed evidence because a later launch failed. It does not automatically repeat mutations after ambiguous failures; the caller may retry the exact request, which lets the helper reconcile that operation.

## One helper process per import

The client runs `agy-import import --input - --json` directly with an argument vector and supplies a UTF-8 JSON document on stdin. It forwards an explicit store override when configured. It does not launch a shell, put message text in arguments, or change directory to an unverified source path.

The helper validates the request and target, reconciles previous intent, installs or returns the session, and verifies persistence before responding. `doctor`, `capabilities`, and `status` are diagnostic conveniences. Consumers do not need to coordinate them to create a session safely.

The helper honors the existing `ANTIGRAVITY_HOME` parent-directory convention itself. `showagent` therefore does not need a new hard-coded storage path for ordinary use. `Options.Store` is an explicit direct-store override for alternate profiles and isolated tests; see the protocol's resolution rules.

The wrapper drains stdout and stderr, retains bounded diagnostics, rejects multiple response documents, and honors context cancellation. On Linux and macOS it terminates the helper's process group; on Windows it terminates the direct helper, without claiming descendant-tree cleanup. Inherited-pipe waits are bounded. Protocol limits and error codes are in [protocol.md](protocol.md). Diagnostic messages must not echo transcripts. A transport failure and a structured helper error remain distinguishable.

## Executable discovery and installation

Resolution order is deterministic:

1. `Options.Executable`, interpreted relative to the caller's original working directory if it contains a relative path. A bare command name is resolved through `PATH`.
2. `AGY_IMPORT_BIN`, using the same interpretation.
3. `agy-import` beside the resolved running consumer executable, or `agy-import.exe` on Windows.
4. `PATH`.

A configured override that is invalid produces an error rather than silently falling through to another executable. Resolve the selected executable to an absolute path before starting it. Do not independently search the imported workspace or current directory; reject implicit current-directory resolution from `PATH`. Symlinks and Windows suffix behavior need native tests.

Ship platform release archives and checksums. A user can install the helper once on `PATH`, or a `showagent` package can place a pinned helper next to its own executable. The client must not download, replace or update a helper during import. Error messages point to the release page and identify the missing/unsupported component.

## Independent client module

The `client/` directory is its own module, `github.com/drolevar/agy-import/client`, with no third-party requirements. It contains both protocol types and the process wrapper. A separate module keeps native-storage dependencies out of the consumer's module graph, not merely out of its linked executable.

Root binary releases use tags such as `v0.x.y`; nested client-module releases use `client/v0.x.y`. The helper protocol, client API and native adapter versions have separate compatibility meanings. Every binary release that advertises protocol v1 must pass the client contract fixtures. A consumer pins a client release and can select any helper release that implements the required protocol and target adapter.

## Changes required in showagent

| Change | Reason |
| --- | --- |
| Map selected `[]Turn` to `[]client.Message` | Reuse source extraction and selection |
| Pass context and stable operation ID through the writer request | Preserve cancellation and existing import-plan identity |
| Invoke the client and map its result to `Row` | Keep the provider adapter small |
| Distinguish writer command `agy-import` from resume command `agy` | Existing `WritesThroughCLI` checks the provider's own command |
| Preserve uncertain external-write outcomes | The helper may have committed before process failure |
| Enable the target only with suitable helper support | Replace the unconditional source-only refusal |
| Test fresh discovery and transcript/workspace recovery | A returned row alone does not prove restart behavior |

The [current import flow](https://github.com/drolevar/showagent/blob/6a2756ac1556fdbb8ca8932c249bc57558287e7b/internal/session/import.go) has an operation ID that is not yet passed into `WriteConverted`. Make that request-context change explicitly. The simpler `Convert` path can use the client's deterministic default identity.

Resume remains `agy --conversation ID` from the verified destination workspace, under the caller's existing resume policy. Neither client nor helper auto-resumes as part of import. No patch to `showagent` is included in this client milestone.
