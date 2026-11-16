# agy-import Go client

A small, independent Go module for calling the external `agy-import` helper. It uses only the standard library and keeps native SQLite/protobuf dependencies out of the consumer's module graph.

**Implementation status:** the client and fake-helper contract tests are provided here. This repository does not yet provide a working native importer executable or a verified Antigravity storage adapter. Real imports require such a helper; the client reports a missing helper explicitly. There is no published client release tag yet.

## API

```go
import (
    "context"
    "errors"
    "fmt"

    agyimport "github.com/drolevar/agy-import/client"
)

func transfer(ctx context.Context, workspace string) error {
    importer := agyimport.New(agyimport.Options{})
    result, err := importer.Import(ctx, agyimport.Request{
        Source: agyimport.Source{
            Application:    "showagent",
            Provider:       "codex",
            ConversationID: "source-session-id",
            Selection:      "all",
        },
        Workspace: workspace, // An absolute path on this computer.
        Messages: []agyimport.Message{
            {Role: "user", Text: "Use Parquet for the datasets."},
            {Role: "assistant", Text: "I will use Parquet and JSON metadata."},
        },
    })
    if err != nil {
        var importErr *agyimport.Error
        if errors.As(err, &importErr) {
            // Retain this identity when retrying or reporting an interrupted import.
            fmt.Printf("Import %s: %s (%s)\n",
                importErr.OperationID, importErr.Code, importErr.Outcome)
        }
        return err
    }
    fmt.Printf("Conversation %s: %s\n", result.ConversationID, result.TranscriptPath)
    return nil
}
```

For `showagent`, map `result.TranscriptPath` to `Row.File`, retain `result.Workspace`, and use the existing `agy --conversation ID` resume operation separately. `result.DatabasePath` is deliberately a different field.

`Preview(ctx, request)` uses `import --dry-run` and returns a `Plan`. `RequestDigest(request)` exposes the portable semantic digest without requiring a native helper. Normal calls construct their own protocol envelope; consumers do not set wire-version strings or compose command flags.

## Configuration and discovery

`Options.Executable` overrides executable discovery; `Options.Store` supplies an explicit direct-store path. Both are optional. Discovery checks:

1. `Options.Executable`.
2. `AGY_IMPORT_BIN`.
3. `agy-import` beside the resolved consumer executable (`agy-import.exe` on Windows).
4. `PATH`, excluding implicit current-directory resolution.

An invalid configured override is an error, not permission to use another executable. No shell is used. Message text is sent on stdin and is not inserted into command-line arguments. The helper inherits the process environment, including the documented `ANTIGRAVITY_HOME` override, and is responsible for resolving the native store.

## Retry and error semantics

Supply `Request.OperationID` when the caller already has a stable import-plan ID. Otherwise the client derives `sha256:<digest>` from the semantic request before launching the helper. The same request produces the same default identity. A different explicit identity requests a separate operation.

Errors are `*Error` values with `Code`, `Outcome`, `OperationID`, an optional `ConversationID`, and an underlying cause. Context cancellation/deadline causes remain accessible with `errors.Is`. Bounded helper diagnostics are available separately and are not included automatically in the error string.

`not_started` means the current invocation did not start native publication. It does not establish that a previous attempt under the same ID never committed. A started process with no trustworthy final response is conservatively `uncertain`. The client does not automatically retry a mutation; repeat the same request/ID to let the helper reconcile it.

If the helper returns a valid result but exits unsuccessfully, the client returns both the result and a `protocol_error`. A `created` result supports `committed`; an `already_exists` result is retained as evidence of the earlier operation while the current invocation is classified `uncertain`.

On Linux and macOS, cancellation terminates the child process group. On Windows, cancellation terminates the direct helper process; descendant-tree termination is not currently guaranteed. Pipe waits are bounded so inherited handles do not indefinitely block cancellation. The native helper is expected to complete one operation itself, without leaving background workers.

## Verification

From the repository root:

```sh
go -C client test -race -shuffle=on ./...
go -C client vet ./...
go -C client list -m all
```

The final command should list only this module. The module's tests and fixtures are self-contained; they do not require files outside `client/`, a real Antigravity installation, Google credentials, or a network model. Fixture copies are checked against the repository's public protocol examples in CI.

CI runs the process-contract tests on Linux, Windows, and macOS. These checks establish client/transport behavior, not native Antigravity compatibility. The module's Go language floor is 1.22; use a currently supported Go compiler for development.

For the complete request/result contract, see the repository's [protocol specification](https://github.com/drolevar/agy-import/blob/docs/consumer-contract/docs/protocol.md).

## License

[MIT](LICENSE).
