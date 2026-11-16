package client

// Protocol constants identify the process contract implemented by this client.
const (
	ProtocolName       = "agy-import"
	ProtocolVersion    = 1
	RepresentationText = "user_assistant_text"
)

// Protocol limits are byte counts except for MaxMessages.
const (
	MaxRequestBytes     = 64 << 20
	MaxMessageBytes     = 16 << 20
	MaxMessages         = 100_000
	MaxResponseBytes    = 1 << 20
	MaxDiagnosticsBytes = 64 << 10
)

// Outcomes describe the current invocation, not all earlier attempts under the
// same operation ID. In particular, NotStarted does not prove that an earlier
// attempt did not commit.
const (
	OutcomeNotStarted = "not_started"
	OutcomeCommitted  = "committed"
	OutcomeUncertain  = "uncertain"
)

// Error codes are stable machine-readable strings. A helper may return an
// additional code; callers should handle unknown codes as ordinary errors.
const (
	CodeInvalidRequest      = "invalid_request"
	CodeIncompleteHistory   = "incomplete_history"
	CodeWorkspaceInvalid    = "workspace_invalid"
	CodeUnsupportedProtocol = "unsupported_protocol"
	CodeUnsupportedTarget   = "unsupported_target"
	CodeTargetBusy          = "target_busy"
	CodeOperationConflict   = "operation_conflict"
	CodeWriteFailed         = "write_failed"
	CodeVerificationFailed  = "verification_failed"
	CodeOutcomeUncertain    = "outcome_uncertain"
	CodeHelperNotFound      = "helper_not_found"
	CodeHelperStartFailed   = "helper_start_failed"
	CodeProtocolError       = "protocol_error"
)

// Options configures the local helper. Empty fields select default executable
// discovery and the helper's default Antigravity CLI store.
type Options struct {
	Executable string
	Store      string
}

// Request supplies selected historical messages to import as a new session.
// The client builds the process envelope; callers do not set a protocol version.
// A missing OperationID is derived from the semantic request before launch.
// Reuse the same request or explicit OperationID after an uncertain outcome.
type Request struct {
	OperationID           string
	Source                Source
	Workspace             string
	Title                 string
	Messages              []Message
	Truncated             bool
	Redacted              bool
	AllowTruncatedHistory bool
}

// Source is optional provenance. Its workspace is not a destination path.
type Source struct {
	Application    string `json:"application,omitempty"`
	Provider       string `json:"provider,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Workspace      string `json:"workspace,omitempty"`
	Selection      string `json:"selection,omitempty"`
	Revision       string `json:"revision,omitempty"`
}

// Message is one selected historical user or assistant message. Text is kept
// exactly as supplied; selection and normalization belong to the producer.
type Message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// Warning reports a known limitation without changing message text.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Verification reports checks performed during this invocation. NativeResume
// is "not_run": normal import does not start a model turn or resume the agent.
type Verification struct {
	Storage             string `json:"storage"`
	TranscriptRoundTrip string `json:"transcript_round_trip"`
	NativeResume        string `json:"native_resume"`
}

// Result describes a created or previously imported conversation. TranscriptPath
// points to the JSONL display mirror; it is distinct from DatabasePath.
type Result struct {
	OperationID     string       `json:"operation_id"`
	ConversationID  string       `json:"conversation_id"`
	Workspace       string       `json:"workspace"`
	TranscriptPath  string       `json:"transcript_path"`
	DatabasePath    string       `json:"database_path"`
	MessagesWritten int          `json:"messages_written"`
	Status          string       `json:"status"`
	Representation  string       `json:"representation"`
	Warnings        []Warning    `json:"warnings"`
	Verification    Verification `json:"verification"`
}

// Plan is a preview result. It does not reserve a conversation ID or create
// files in the destination store.
type Plan struct {
	OperationID     string    `json:"operation_id"`
	Workspace       string    `json:"workspace"`
	Representation  string    `json:"representation"`
	MessagesPlanned int       `json:"messages_planned"`
	Warnings        []Warning `json:"warnings"`
}

// Error preserves a stable machine code, invocation outcome, operation identity
// and underlying cause. Diagnostics is bounded helper stderr and is excluded
// from Error(); inspect it deliberately rather than including it in routine logs.
type Error struct {
	Code           string
	Outcome        string
	OperationID    string
	Message        string
	ConversationID string
	Cause          error
	Diagnostics    string
}

// Error returns the stable code and human-readable description. It deliberately
// excludes underlying cause text and helper diagnostics, which can contain data.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Code == "" {
		return ProtocolName + ": " + e.Message
	}
	if e.Message == "" {
		return ProtocolName + ": " + e.Code
	}
	return ProtocolName + ": " + e.Code + ": " + e.Message
}

// Unwrap preserves cancellation, deadlines and other underlying causes for
// errors.Is and errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
