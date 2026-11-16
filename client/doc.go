// Package client calls an external agy-import executable using the version 1
// import protocol. It supplies typed requests and results, helper discovery,
// stable operation identities, bounded process I/O, and context cancellation.
//
// This module uses only the Go standard library. It does not open Antigravity
// databases, encode native protobuf records, or start a model turn. A compatible
// helper must be installed separately; the client never downloads one.
//
// Import and Preview each invoke the helper once. Diagnostic commands are not
// prerequisites. If an import's outcome is uncertain, retry the same request and
// operation ID so the helper can reconcile it. The client does not automatically
// retry mutations.
package client
