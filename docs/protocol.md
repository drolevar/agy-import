# Import protocol v1

This document defines the proposed first public process contract. No importer or client is implemented in this revision. The JSON schemas and examples are reviewable specification artifacts; v1 is not frozen until its first release.

## Transport

Run `agy-import import --input - --json` and send one UTF-8 JSON request on stdin. A filename may replace `-` for manual use. Stdout contains exactly one JSON response and optional surrounding whitespace. Stderr contains bounded diagnostics and must not echo conversation contents. An explicit `--store PATH` selects the directory that directly contains `conversations/` and the CLI summary store. Without it, the helper honors showagent's `ANTIGRAVITY_HOME` convention as the parent of the Antigravity surfaces and appends `antigravity-cli`; without that override, use the platform user's `.gemini/antigravity-cli` directory. Resolve the destination before opening files. The store selection is configuration, not a path taken from transcript text.

The normal operation performs request validation, target detection, version/schema checks, retry reconciliation, installation and verification. No preceding diagnostic command is required. `import --dry-run` returns a plan and does not create directories, databases, journals, or receipts in the target store. It must also use target read paths that do not create WAL/SHM sidecars. If that cannot be established, fail without touching the store. A preview is not a reservation; import rechecks its assumptions.

Reject duplicate JSON object keys at every depth and invalid Unicode, including unpaired UTF-16 surrogate escapes such as `"\uD800"`. Decode valid escapes to Unicode scalar-value strings before validation and hashing. Do not repair invalid strings with replacement characters. These parsing rules apply before JSON Schema validation and keep intent and digests consistent across languages.

Exit codes are coarse: `0` for `created`, `already_exists`, or `planned`; `1` for a structured operational error; `2` for invalid CLI usage or request syntax. CLI usage errors may have no JSON response. Machine decisions use the validated response and error outcome, not the exit code alone. A success document with a nonzero exit is not accepted as normal success: preserve any known committed result as diagnostic evidence and report the transport discrepancy.

## Request

See [the request schema](../schema/import-request-v1.schema.json) and [the example](../examples/import-request.json).

| Field | Requirement and meaning |
| --- | --- |
| `protocol` | Required, exactly `agy-import` |
| `protocol_version` | Required, integer `1` |
| `operation_id` | Required stable caller identity; 1–128 ASCII letters, digits, `.`, `_`, `:`, or `-`, beginning with a letter or digit |
| `source` | Optional provenance object; does not select a parser or grant authority |
| `target.workspace` | Required absolute workspace path in the destination environment |
| `target.title` | Optional display title; imported history never becomes an instruction to generate it |
| `history.representation` | Required, exactly `user_assistant_text` |
| `history.messages` | Required nonempty ordered array of `{role, text}`; role is `user` or `assistant` |
| `history.truncated` | Required; indicates unintended incompleteness within the declared selection |
| `history.redacted` | Required; indicates producer-applied redaction |
| `policy.allow_truncated_history` | Optional, defaults to false |

Optional source fields are `application`, `provider`, `conversation_id`, `workspace`, `selection`, and `revision`. Omitted optional strings normalize to empty strings for identity. A deliberate selected prefix or last-N window is described by `source.selection` and is not marked truncated solely because it excludes other turns. Unknown source metadata is absent; source timestamps are not fabricated. Message text is preserved as received, including newlines, whitespace and Unicode. The importer does not merge consecutive equal roles, silently reorder messages or infer tools from prose.

A truncated request is rejected with `incomplete_history` unless its policy explicitly accepts it. Accepted truncation and redaction produce warning codes. The helper does not reverse redaction. With absent provenance, generic imports still work but cannot be attributed to an original session.

The workspace is supplied independently of `source.workspace`. Validate that it is an existing destination-local directory. Store its verified local identity and native file URI according to the supported adapter. Do not rewrite paths inside message text or copy project files. Supported scope is creation of a new conversation; append/update is not a v1 operation.

## Operation identity and request digest

The helper stores operations in a ledger scoped to the resolved destination store. An explicit operation ID from a caller is retained verbatim. The planned Go client can derive an ID when its caller omits one: `sha256:` followed by the lowercase hexadecimal request digest. It does this before starting the child, so even a transport error carries a known operation ID.

The helper independently computes the same digest for every request. A given operation ID may not be reused with a different digest. The ID is not an Antigravity conversation UUID; the helper allocates and durably records native IDs separately. A caller can intentionally request another copy using a fresh explicit operation ID.

Operation IDs are opaque ledger keys, never literal filenames. In particular, the colon in `sha256:...` is not valid in a Windows filename. Encode or hash keys for any filesystem representation inside the helper; callers do not need to know that mapping.

To make hashing independent of JSON key order and whitespace, hash a fixed sequence of UTF-8 tokens. For each token, append its UTF-8 byte length in unsigned base-10 ASCII with no leading zeros, a colon, then the token's bytes. Concatenate these framed tokens and calculate SHA-256. Empty string encodes as `0:`. Do not apply Unicode normalization, newline conversion, filesystem canonicalization, or JSON escaping to token bytes.

Token order is:

1. Literal `agy-import:request:1`.
2. Source `application`, `provider`, `conversation_id`, `workspace`, `selection`, `revision`, in that order; omitted values are empty strings.
3. Target `workspace`, then `title` (empty if omitted).
4. History representation, then truncated and redacted flags, each Boolean as `1` or `0`.
5. Message count as unsigned base-10 ASCII with no leading zeros.
6. For each message in order: role, then text.
7. `policy.allow_truncated_history` as `1` or `0`, defaulting to `0`.

The protocol envelope and operation ID are excluded; the first token already identifies the protocol version. Store configuration is excluded because the ledger is store-scoped. A different path spelling or changed provenance/title changes the digest even if a human considers it equivalent. Reconciliation must also recheck the destination workspace's actual identity; an alias that resolves elsewhere after the first attempt is a conflict. Input rendering changes alone do not change the digest. The checked-in [digest fixture](../examples/request-digest.json) gives an exact example.

For the same ID and digest, a completed import is verified and returned as `already_exists`; interrupted operations are reconciled using their original native IDs. If the target session has continued, verify that the imported history remains its prefix and leave the appended turns intact. Changed or missing artifacts produce a conflict/uncertain result according to the recoverable journal state. Do not allocate a second session merely because the previous response was lost.

## Responses

See [the response schema](../schema/import-response-v1.schema.json). Every response includes protocol/version, status and operation ID when the request provided a syntactically valid one. A malformed request may use a null operation ID in its error response.

| Status | Meaning |
| --- | --- |
| `created` | New session persisted and structurally verified |
| `already_exists` | Previous matching operation verified and returned |
| `planned` | Dry-run checks completed; no target write or ID reservation |
| `error` | Structured failure with explicit persistence outcome |

Created/existing results include `conversation_id`, `workspace`, `database_path`, `transcript_path`, `messages_written`, representation, warnings and verification. Paths are absolute and separately identified. `messages_written` counts original imported messages even if the session subsequently continued. The client checks it against the request count. The transcript path must be suitable for the current `showagent` JSONL reader. A database path must never be substituted for it.

`verification.storage` and `verification.transcript_round_trip` must be `passed` on success. `verification.native_resume` is `not_run`: normal import does not launch a model or resume the native agent. Compatibility evidence belongs to the adapter's test/release manifest. The helper may invoke a verified local version-information command during detection, but must not create a model turn.

Plans contain workspace, representation, `messages_planned` and warnings. They do not promise a conversation ID or persisted paths. Warnings are objects with a stable `code` and a human-readable `message`; consumers tolerate additional fields and unknown warning codes.

## Errors and transport failures

An error contains a stable string `code`, human-readable `message`, and `outcome`. Outcomes describe the current invocation's persistence effects; they are not a complete history of every attempt under the operation ID:

| Outcome | Meaning |
| --- | --- |
| `not_started` | This invocation has not published a new session or changed shared native metadata |
| `committed` | This invocation completed persistence, but later receipt/reporting work failed |
| `uncertain` | This invocation's persistence effects cannot be determined reliably; retry the same operation to reconcile |

Private staging or an intent journal may exist even for `not_started`; it does not mean absolutely no filesystem activity. A previous attempt under the same ID may already have committed. A later `not_started` error must not erase earlier uncertain/committed evidence or justify switching to a fresh ID. Where the helper knows a previous operation is complete, it normally returns `already_exists`; an error that can identify the target may include `conversation_id`. Message bodies, credentials and unrelated store contents do not belong in diagnostics.

Initial codes are `invalid_request`, `unsupported_protocol`, `unsupported_target`, `target_busy`, `workspace_invalid`, `incomplete_history`, `operation_conflict`, `write_failed`, `verification_failed`, and `outcome_uncertain`. The Go client additionally reports `helper_not_found`, `helper_start_failed`, and `protocol_error`. Future codes are additive and must remain generically handleable.

If the process cannot start, the client can report `not_started`. If it started and produced no valid final response, a signal, deadline, nonzero exit or broken pipe does not prove rollback; default to `uncertain`. Preserve operation ID and context cancellation/deadline causes. Do not automatically retry a mutation after an ambiguous transport failure. Retrying the same request delegates reconciliation to the helper; the normal caller need not implement `status` orchestration.

## Limits and evolution

The first implementation must bound input to 64 MiB of UTF-8 JSON, message count to 100,000, and each message's text to 16 MiB of UTF-8. Exceeding a limit is an error with no silent truncation. JSON Schema checks structural limits; byte limits, absolute paths and native invariants require runtime validation. Consumers retain at most 1 MiB of stdout and 64 KiB of stderr and continue draining streams or terminate promptly on overflow without pipe deadlocks.

Requests reject unknown fields so misspellings cannot silently change intent. Adding request fields requires a new negotiated protocol version; a consumer using v1 always sends the v1 shape. Responses permit additive fields and warning/error codes. Unsupported protocol versions fail explicitly. Capability/diagnostic commands can describe support, but import itself is authoritative and performs its own checks.

Protocol version, Go client API version and Antigravity adapter version are independent. Nothing in the public request exposes protobuf field numbers, SQL table definitions or backup/recovery internals.
