# Antigravity schema compatibility

**Status: native guard design; not implemented.** The [Go client](../client/)
validates the public process contract and returns typed helper errors. It does
not identify Antigravity builds, inspect native storage, or authorize native
writes. The supported-native-build set is currently empty.

This document makes the [architecture](architecture.md) requirements concrete
for the native adapter and installer milestones in the
[implementation plan](implementation-plan.md). The goal is controlled refusal
when compatibility is unknown, with native evidence for every allowed combination.

## The write gate

Every import must match a verified compatibility profile before publishing
native data. The profile binds all of the following; no single match is sufficient.

| Requirement | Evidence recorded by the profile |
| --- | --- |
| Native implementation | Exact tested build/version, OS, architecture, and executable or package digest |
| SQLite structure | Relevant tables, columns, constraints, indexes, triggers, and meaningful format markers |
| Protobuf representation | Supported message/record variants, field mappings, and semantic invariants |
| Auxiliary storage | Required summaries, workspace metadata, indexes, and transcript mirrors |
| Installer and recovery | Adapter identity, supported store generations, and journal/recovery format |
| Native compatibility | Reproducible results from the exact native-build tests below |

The initial policy accepts exact tested builds. A version range such as
`version >= X` must not substitute for compatibility evidence. If `agy` is a
launcher, identify its effective implementation; the launcher's digest alone
does not identify the code that reads and writes sessions.

Check the actual destination independently of the installed binary. A known
binary may encounter a store migrated by another build, a partially migrated
profile, or records from different generations. Inspect the shared metadata and
artifacts the operation will touch. Do not infer the entire profile from one old
conversation database or hash all conversation contents as a schema fingerprint.

An empty store needs its own tested initialization path. Missing databases are
not evidence of a compatible format. Unknown combinations return
`unsupported_target` before native publication. The importer must not downgrade
or automatically migrate a user store merely to fit an older adapter.

## What fingerprints can establish

Use a stable description of the relevant SQLite structure rather than a hash of
database bytes, user data, or physical page locations. Include generated and
hidden columns where applicable: SQLite's
[`table_xinfo`](https://sqlite.org/pragma.html#pragma_table_xinfo) includes columns
that `table_info` omits. Unexpected additions are allowed only when the profile
explicitly supports their effect on the intended write.

SQLite's [`schema_version`](https://sqlite.org/pragma.html#pragma_schema_version)
is an internal schema-change counter, not an Antigravity format version.
[`user_version`](https://sqlite.org/pragma.html#pragma_user_version) is available
for application-defined use; an adapter must establish its actual meaning before
using it as evidence.

A matching SQL layout cannot establish compatibility of protobuf BLOB contents.
The [protobuf wire format](https://protobuf.dev/programming-guides/encoding/)
stores field numbers, wire types, and payloads; several logical types share a
wire type. Observed tags do not reveal absent optional fields or prove unchanged
application meaning. Successful decoding is therefore only a structural check.

Pin relevant protobuf definitions/descriptors when available. Otherwise record
the understood mappings and their limitations explicitly. Both approaches need
native semantic tests; neither can certify arbitrary future builds automatically.

## Preserve unrelated protobuf data

Update shared metadata through binary protobuf handling that preserves unknown
fields. The [protobuf documentation](https://protobuf.dev/programming-guides/proto3/#unknown-fields)
explains that JSON conversion and rebuilding messages from recognized fields can
lose unknown data. Test preservation at the nested records the adapter modifies.

Preservation does not prove that a modification is semantically valid. The
adapter must distinguish records it understands and may change, opaque content
it can leave untouched, and structures it cannot safely update. Refuse the last
category. Construct fresh imported records from the verified format; do not clone
a populated user conversation and replace its text.

## Inspection and publication

The normal helper import performs these checks itself. A cached successful
`doctor`, `capabilities`, or preview result never authorizes a later write.

1. Identify the effective native build and resolved destination profile.
2. Inspect the destination without changing it and select a verified profile.
3. Establish destination quiescence and the appropriate available locks.
4. Record intent, construct the new session in staging, and validate its artifacts.
5. Recheck the build, store generation, and relevant artifact identities before publication.
6. Publish through the recoverable installer, read back the result, and record its receipt.

Detection must not open an unknown profile through Antigravity just to discover
its format: native startup may initialize or migrate it. Use installed-artifact
identity and only verified version-information paths that leave the destination
unchanged. Normal import must not start a model turn.

Use a tested read-only inspection or consistent snapshot procedure that includes
WAL state. SQLite documents that a
[read-only WAL connection](https://sqlite.org/wal.html#read_only_databases) can
depend on existing sidecars or create them where directory permissions allow.
Do not use [`immutable=1`](https://sqlite.org/uri.html#uriimmutable) against a
potentially changing live store: it disables locking and change detection.
If non-mutating inspection cannot be established, refuse without touching the store.

Quiescence means the relevant native profile is not being modified. A lock used
only by agy-import does not stop Antigravity or its updater, and a SQLite write
lock does not coordinate separate protobuf files or application metadata cached
in memory. Rechecks reduce races but do not establish exclusivity against an
uncooperative writer. The supported operating conditions must exclude such
concurrent mutation where reliable coordination is unavailable; otherwise return
`target_busy` before publication.

## Recovery after an upgrade

The journal retains the operation ID, request digest, original native IDs,
adapter identity, detected build/store generation, and artifact ownership and
verification data. These internal details do not alter the public request digest.

An exact retry must revalidate compatibility before replaying installation steps.
If Antigravity changed after an interrupted import, use a recovery path explicitly
verified for that transition or stop and preserve the journal and artifacts.
Never restore an old shared metadata snapshot wholesale over a store that has
advanced, or remove an imported conversation that has subsequently continued.

Backups support recovery; they do not justify an incompatible write. Use
[SQLite-aware snapshots](https://sqlite.org/backup.html), since committed data may
still reside in the [WAL](https://sqlite.org/wal.html#the_wal_file). Multi-file
publication still requires a journal; an individual database transaction does
not make the entire native store atomic.

Report outcomes according to the [protocol](protocol.md#errors-and-transport-failures).
`not_started` describes this invocation and does not erase earlier partial or
committed work under the same ID. After publication, an unresolved result remains
`uncertain`; a known completed persistence step followed by reporting failure is
`committed`. Preserve the operation ID so the caller can retry without duplication.

## Certifying a profile

Use synthetic sessions in an isolated profile with the exact native build. Record
artifact digests, platform, adapter revision, fixture identity, and test results.

| Test | Required result |
| --- | --- |
| Native opening | Antigravity accepts the imported conversation |
| History replay | A controlled test provider receives the imported historical markers |
| Continuation | A new turn persists in the original imported conversation |
| Reopen | The continued conversation remains usable after restart |
| Fresh showagent discovery | Correct conversation ID, workspace, transcript path, and resumability |
| Preservation | Unrelated sessions and shared metadata remain intact |
| Import isolation | Import itself makes no model request and changes no project files |

Negative cases must cover unexpected columns/constraints/triggers, incompatible
protobuf variants, unknown fields in modified structures, mixed or incomplete
format generations, target changes before publication, and interrupted imports
followed by an upgrade. Assert refusal or explicitly supported recovery, including
accurate outcomes and preservation of earlier operation evidence.

Writer/reader round trips, SQLite integrity checks, and client CI are useful but
do not replace native continuation evidence. Native continuation runs during
adapter certification; production import retains `verification.native_resume`
as `not_run` and performs storage/transcript checks without a model call.

## Consumer behavior and maintenance

Showagent keeps one `Import(ctx, request)` call. An unrecognized build or store
profile produces a typed `unsupported_target` error with the operation ID and an
actionable message. No consumer-side schema detection or diagnostic orchestration
is required; see [showagent integration](showagent-integration.md).

A harmless upstream rebuild may need only a new verified build entry. A storage
change may require a new adapter and recovery rules. Add either only after the
relevant gates pass. Public protocol, client API, and native adapter versions
remain independent, so an Antigravity storage update normally changes the helper
without changing showagent's selected-message request.
