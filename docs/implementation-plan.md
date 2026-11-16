# Implementation plan

This plan tracks the complete native-import goal. The consumer contract and independent Go client are implemented in this checkout; the client is exercised against a fake helper. No native importer executable is shipped, no client release is tagged, and no native Antigravity compatibility has been verified by this project.

| Milestone | Current state |
| --- | --- |
| Consumer contract | Documented, with schemas and shared examples; proposed v1 remains unreleased |
| Independent Go client | Implemented, with request, discovery, transport, and fake-helper tests |
| Native storage profile and installer | Pending |
| Real native continuation and showagent discovery | Not run |
| Showagent adapter and release packaging | Pending |

## Outcome

The first usable release must let showagent create a new Antigravity conversation through one typed client call. The imported session must be discoverable with the correct workspace, readable as a transcript, accepted by the real Antigravity CLI, and usable when continuing the conversation. Installation must not start a model turn.

Each milestone below has a concrete acceptance gate. Report synthetic checks, actual native-client checks, and platform coverage separately. Do not turn a successful Go cross-compilation into a runtime compatibility claim.

## 1. Establish the consumer contract

Complete the [protocol](protocol.md), [showagent integration](showagent-integration.md), JSON schemas, and synthetic examples before adding native persistence. Keep the version-1 representation limited to ordered user/assistant text.

The contract must define:

- One request and one structured response per helper invocation.
- A single normal import operation that performs validation, compatibility checking, installation, and recovery internally.
- Separate source provenance and destination-local workspace.
- Explicit transcript fidelity and verification results.
- Stable operation identity, exact-retry behavior, and content-conflict behavior.
- Typed errors with a distinction between no change and uncertain outcome.
- Separate database and transcript paths in successful results.

**Gate:** all example requests/results validate against their schemas, documentation agrees with the schemas, and one ordinary showagent conversion can be expressed without a diagnostic-command prerequisite or Antigravity schema knowledge.

## 2. Implement the independent Go client

The [client module](../client/) now provides `New`, `Import`, `Preview`, and `RequestDigest`, with typed requests, results, and errors. Its tests are self-contained and use only synthetic data. The CI workflow runs on Linux, Windows, and macOS and separately checks that the module has no third-party dependencies. See the client README for reproduction commands and the workflow run for the results on each platform.

Create `github.com/drolevar/agy-import/client` as its own Go module using only the standard library. Provide request/result types and a normal `Import(ctx, request)` call. Share the wire types with the helper without making the client depend on native storage packages.

Implement explicit-path, sibling-executable, and `PATH` discovery as documented. Keep discovery deterministic and return actionable missing-helper errors. Accept an existing operation ID or derive the documented canonical `sha256:<digest>` identity before process launch. Spawn the helper directly, send JSON on stdin, and bound captured output. Preserve operation identity and cancellation causes alongside the import outcome classification. Do not retry mutations automatically.

Use a fabricated helper executable to verify transport behavior before a native writer exists. Important cases are a valid success receipt, structured failure, malformed or excessive output, unsupported protocol, cancellation, and a child that commits a simulated result before its response is lost. The client must not interpret an empty or malformed response as proof that nothing happened.

**Gate:** a small Go consumer imports only the client module and exercises the entire process contract with fixtures; its dependency graph contains no SQLite/protobuf runtime. This gate establishes client behavior, not native import support.

## 3. Establish one current native storage profile

Choose an exact installed Antigravity CLI build on the initial development platform and record its version, architecture, executable digest, and observed schema. Use synthetic sessions in an isolated profile to identify required step records, protobuf fields, IDs, summary metadata, workspace representation, and transcript mirrors.

Implement the [schema compatibility policy](schema-compatibility.md) as the native writer's mandatory gate. Selection requires both a verified build and a matching actual store profile; recovery must retain and revalidate the adapter and format generation that began the operation.

The existing [session-migrate format research](https://github.com/xhluca/session-migrate/blob/45351ff95ce79684c23377920b92026ecbe9759b/docs/antigravity-format.md) and [interoperability fixture project](https://github.com/xhluca/antigravity-session-interoperability) are reference material. Their proven target is Antigravity CLI 1.1.16 on Linux x86-64. Verify current behavior rather than silently extending that claim to another release. Preserve attribution and applicable license notices for reused material.

Start with user and assistant text. Native requirements for timestamps and other metadata must be distinguished from unavailable original source metadata. If the writer creates timestamps, label them as generated rather than source facts.

**Gate:** documented field mappings and wholly synthetic fixtures produce a structurally valid conversation for the selected build, with a deterministic decoded message sequence and explicit handling of unsupported records.

## 4. Implement a recoverable installer

Build a staged create-only installer around the verified adapter. Add destination quiescence checks, consistent backups for affected shared metadata, create-if-absent publication, a durable operation journal, read-back verification, and receipts.

The wire request always supplies an operation identity, either passed through from the caller or derived by the Go client before launch. The helper independently checks the payload digest and scopes its operation ledger to the canonical destination store. Identical retries reconcile the existing operation. A changed request under the same identity conflicts. New source revisions create new imported sessions and do not overwrite continued target conversations.

Test interruptions at meaningful boundaries: before publication, after conversation publication, after index updates, and before receipt delivery. Verify that recovery does not duplicate a conversation, damage unrelated indexes, or remove a target that has advanced since import. A SQLite transaction alone does not satisfy the multi-file recovery gate.

**Gate:** every interrupted synthetic operation can be reconciled by the same import request or returns a precise unresolved-outcome error without unsafe cleanup. No real user store is used by these tests.

## 5. Prove the native vertical slice

Import a synthetic conversation containing distinctive markers into an isolated profile. Then verify all of the following with the exact target build:

| Check | Required evidence |
| --- | --- |
| Persistence | Expected SQLite and protobuf records decode correctly |
| Fresh showagent discovery | Correct ID, workspace, transcript path, and resumability |
| Native opening | Antigravity accepts and displays the imported history |
| History replay | A controlled provider receives imported markers on continuation |
| Continued persistence | New user and assistant messages are saved in the same conversation |
| Reopen | The continued conversation remains usable after restarting the client |
| Import isolation | The creation operation itself invokes no model and changes no project files |

The [session-migrate native integration test](https://github.com/xhluca/session-migrate/blob/45351ff95ce79684c23377920b92026ecbe9759b/tests/test_antigravity_native.py) demonstrates a useful local-provider test pattern. Its existing pass does not replace this project's native gate.

**Gate:** publish a compatibility record identifying the exact binaries, OS/architecture, fixture, and outcomes. Mark any unperformed check as not run.

## 6. Add the thin showagent adapter

Adapt the existing [conversion writer seam](https://github.com/drolevar/showagent/blob/6a2756ac1556fdbb8ca8932c249bc57558287e7b/internal/session/handoff.go) to call the client. Use the already selected conversion turns; do not route full conversion through the bounded, redacted public transcript command.

Pass operation identity and cancellation through the writer request. Separate writer availability (`agy-import`) from resume availability (`agy`). Convert the successful receipt to a showagent row with `Row.File` set to the transcript path. Preserve caller-controlled resume and existing uncertain-write behavior.

**Gate:** the showagent patch contains no Antigravity SQL/protobuf logic and requires no sequence of doctor, status, or repair commands for ordinary conversion and retry.

## 7. Package and release supported combinations

Publish helper binaries and checksums for each tested target platform, with stable filenames that support sibling installation and `PATH`. Version the client module separately using submodule tags such as `client/v0.1.0`; helper releases use their own tags. These tag names are planned conventions, not currently available releases.

Test packaged binaries on their actual operating systems, including executable discovery, paths with spaces, process cancellation, filesystem publication, and native import. Expand the compatibility matrix only when the relevant native checks pass. A later Antigravity build that has not passed those checks must receive an unsupported-target result.

**Release gate:** a new consumer can install the helper, import the client module, and perform one typed import call on a documented supported combination. The release notes state the supported scope and the exact evidence behind it.
