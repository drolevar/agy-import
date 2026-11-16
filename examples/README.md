# Protocol examples

These are synthetic proposed-protocol documents. They were not produced by an implemented importer, and the success example is not native compatibility evidence. Paths and conversation IDs are illustrative.

| File | Purpose |
| --- | --- |
| [import-request.json](import-request.json) | Selected text history with cross-platform source/destination paths and Unicode |
| [import-created.json](import-created.json) | Successful storage result with distinct database/transcript paths |
| [import-planned.json](import-planned.json) | Preview without a reserved conversation ID |
| [import-error.json](import-error.json) | Unsupported native build with no published write |
| [request-digest.json](request-digest.json) | Expected semantic request digest and derived operation ID |

The explicit operation ID in `import-request.json` is legal and is echoed by its response examples. `request-digest.json` also shows the ID a client would derive if its caller omitted that ID. Both choices refer to the same payload digest; they are distinct operation identities and must not be switched midway through a retry.

Validate request/response shapes with the [schemas](../schema/). Runtime validation must additionally enforce the byte limits and semantics in [protocol.md](../docs/protocol.md).
