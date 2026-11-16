package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Client invokes an agy-import executable using the versioned process contract.
// It does not access Antigravity's databases or start a model turn itself.
// A Client can be reused concurrently when the caller does not concurrently
// modify the process environment or working directory.
type Client struct {
	options Options
}

// New stores immutable client options. Executable resolution and validation
// happen when Import or Preview is called; construction starts no process.
func New(options Options) *Client {
	return &Client{options: options}
}

// Import creates or reconciles one conversation through a single helper call.
// It never retries a mutation automatically. Retry the same request and
// operation ID to let the helper reconcile an interrupted attempt.
func (c *Client) Import(ctx context.Context, request Request) (Result, error) {
	response, err := c.invoke(ctx, request, false)
	return response.result, err
}

// Preview asks the helper to validate and plan an import without target writes.
// A preview does not reserve an ID or replace the checks made during Import.
func (c *Client) Preview(ctx context.Context, request Request) (Plan, error) {
	response, err := c.invoke(ctx, request, true)
	return response.plan, err
}

type decodedResponse struct {
	result  Result
	plan    Plan
	failure *Error
}

func (c *Client) invoke(ctx context.Context, request Request, preview bool) (decodedResponse, error) {
	operationID, input, err := prepareRequest(request)
	if err != nil {
		return decodedResponse{}, err
	}
	localError := func(code, message string, cause error) error {
		return &Error{Code: code, Outcome: OutcomeNotStarted, OperationID: operationID, Message: message, Cause: cause}
	}
	if ctx == nil {
		return decodedResponse{}, localError(CodeInvalidRequest, "a non-nil context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return decodedResponse{}, localError(CodeHelperStartFailed, "import canceled before the helper started", err)
	}
	if c == nil {
		return decodedResponse{}, localError(CodeInvalidRequest, "client is nil", nil)
	}
	executable, err := resolveExecutable(c.options.Executable)
	if err != nil {
		return decodedResponse{}, localError(CodeHelperNotFound, "agy-import helper is unavailable; configure its path or install it from https://github.com/drolevar/agy-import/releases", err)
	}
	args := []string{"import", "--input", "-", "--json"}
	if c.options.Store != "" {
		store, err := filepath.Abs(c.options.Store)
		if err != nil {
			return decodedResponse{}, localError(CodeInvalidRequest, "could not resolve the configured destination store", err)
		}
		args = append(args, "--store", store)
	}
	if preview {
		args = append(args, "--dry-run")
	}

	childContext, cancel := context.WithCancel(ctx)
	defer cancel()
	stdout := &boundedCapture{limit: MaxResponseBytes, overflow: cancel}
	stderr := &boundedCapture{limit: MaxDiagnosticsBytes}
	cmd := exec.CommandContext(childContext, executable, args...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// A child that exits while a descendant holds a pipe must not keep the
	// consumer waiting indefinitely. Context deadlines still bound execution.
	cmd.WaitDelay = time.Second
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		return decodedResponse{}, localError(CodeHelperStartFailed, "could not start the agy-import helper", errors.Join(ctx.Err(), err))
	}
	waitErr := cmd.Wait()
	output, tooLarge := stdout.snapshot()
	diagnostics, _ := stderr.snapshot()
	cause := errors.Join(ctx.Err(), waitErr)
	if tooLarge {
		return decodedResponse{}, &Error{
			Code: CodeProtocolError, Outcome: OutcomeUncertain, OperationID: operationID,
			Message: "helper response exceeded the protocol limit; retry the same operation to reconcile",
			Cause:   cause, Diagnostics: string(diagnostics),
		}
	}
	response, decodeErr := decodeResponse(output, operationID, request, preview)
	if decodeErr != nil {
		return decodedResponse{}, &Error{
			Code: CodeProtocolError, Outcome: OutcomeUncertain, OperationID: operationID,
			Message: "helper did not return a valid matching response; retry the same operation to reconcile",
			Cause:   errors.Join(cause, decodeErr), Diagnostics: string(diagnostics),
		}
	}
	if response.failure != nil {
		response.failure.Cause = cause
		response.failure.Diagnostics = string(diagnostics)
		return response, response.failure
	}
	if waitErr != nil {
		outcome := OutcomeCommitted
		conversationID := response.result.ConversationID
		if preview {
			outcome = OutcomeNotStarted
		} else if response.result.Status == "already_exists" {
			// The matching prior operation exists, but this invocation's
			// unsuccessful exit leaves any reconciliation work uncertain.
			outcome = OutcomeUncertain
		}
		return response, &Error{
			Code: CodeProtocolError, Outcome: outcome, OperationID: operationID,
			ConversationID: conversationID,
			Message:        "helper returned a complete response but exited unsuccessfully",
			Cause:          cause, Diagnostics: string(diagnostics),
		}
	}
	return response, nil
}

// boundedCapture always acknowledges the complete write. Once full it discards
// further bytes while os/exec continues draining the pipe. Stdout overflow also
// cancels the child; stderr overflow only limits retained diagnostics.
type boundedCapture struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	limit    int
	exceeded bool
	overflow func()
}

func (w *boundedCapture) Write(p []byte) (int, error) {
	w.mu.Lock()
	remaining := w.limit - w.buf.Len()
	if remaining > len(p) {
		remaining = len(p)
	}
	if remaining > 0 {
		_, _ = w.buf.Write(p[:remaining])
	}
	firstOverflow := len(p) > remaining && !w.exceeded
	if len(p) > remaining {
		w.exceeded = true
	}
	w.mu.Unlock()
	if firstOverflow && w.overflow != nil {
		w.overflow()
	}
	return len(p), nil
}

func (w *boundedCapture) snapshot() ([]byte, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...), w.exceeded
}

type responseEnvelope struct {
	Protocol        *string               `json:"protocol"`
	ProtocolVersion *int                  `json:"protocol_version"`
	OperationID     *string               `json:"operation_id"`
	Status          *string               `json:"status"`
	ConversationID  *string               `json:"conversation_id"`
	Workspace       *string               `json:"workspace"`
	DatabasePath    *string               `json:"database_path"`
	TranscriptPath  *string               `json:"transcript_path"`
	MessagesWritten *int                  `json:"messages_written"`
	MessagesPlanned *int                  `json:"messages_planned"`
	Representation  *string               `json:"representation"`
	Verification    *verificationEnvelope `json:"verification"`
	Warnings        *[]warningEnvelope    `json:"warnings"`
	Failure         *errorEnvelope        `json:"error"`
}

type verificationEnvelope struct {
	Storage             *string `json:"storage"`
	TranscriptRoundTrip *string `json:"transcript_round_trip"`
	NativeResume        *string `json:"native_resume"`
}

type warningEnvelope struct {
	Code    *string `json:"code"`
	Message *string `json:"message"`
}

type errorEnvelope struct {
	Code    *string `json:"code"`
	Message *string `json:"message"`
	Outcome *string `json:"outcome"`
}

// encoding/json matches struct fields without regard to case. The wire
// contract requires exact JSON member names: additive fields must neither
// satisfy nor replace required fields. Decode only exact members at each
// schema object while leaving unrelated additive fields untouched.
func decodeObject(data []byte, fields map[string]any) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if object == nil {
		return errors.New("expected a response object")
	}
	for name, destination := range fields {
		if value, ok := object[name]; ok {
			if err := json.Unmarshal(value, destination); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *responseEnvelope) UnmarshalJSON(data []byte) error {
	if err := decodeObject(data, map[string]any{
		"protocol": &w.Protocol, "protocol_version": &w.ProtocolVersion,
		"operation_id": &w.OperationID, "status": &w.Status,
	}); err != nil {
		return err
	}
	if stringValue(w.Status) == "error" {
		var identity json.RawMessage
		if err := decodeObject(data, map[string]any{"conversation_id": &identity, "error": &w.Failure}); err != nil {
			return err
		}
		if len(identity) > 0 {
			if err := json.Unmarshal(identity, &w.ConversationID); err != nil {
				return err
			}
			if w.ConversationID == nil {
				return errors.New("optional conversation identity must not be null")
			}
		}
		return nil
	}
	fields := map[string]any{
		"workspace": &w.Workspace, "representation": &w.Representation,
		"warnings": &w.Warnings,
	}
	switch stringValue(w.Status) {
	case "created", "already_exists":
		fields["conversation_id"] = &w.ConversationID
		fields["database_path"] = &w.DatabasePath
		fields["transcript_path"] = &w.TranscriptPath
		fields["messages_written"] = &w.MessagesWritten
		fields["verification"] = &w.Verification
	case "planned":
		fields["messages_planned"] = &w.MessagesPlanned
	default:
		return nil
	}
	return decodeObject(data, fields)
}

func (w *verificationEnvelope) UnmarshalJSON(data []byte) error {
	return decodeObject(data, map[string]any{
		"storage": &w.Storage, "transcript_round_trip": &w.TranscriptRoundTrip,
		"native_resume": &w.NativeResume,
	})
}

func (w *warningEnvelope) UnmarshalJSON(data []byte) error {
	return decodeObject(data, map[string]any{"code": &w.Code, "message": &w.Message})
}

func (w *errorEnvelope) UnmarshalJSON(data []byte) error {
	return decodeObject(data, map[string]any{"code": &w.Code, "message": &w.Message, "outcome": &w.Outcome})
}

func decodeResponse(data []byte, operationID string, request Request, preview bool) (decodedResponse, error) {
	if err := validateJSON(data); err != nil {
		return decodedResponse{}, err
	}
	var wire responseEnvelope
	if err := json.Unmarshal(data, &wire); err != nil {
		return decodedResponse{}, errors.New("response fields have invalid types")
	}
	if wire.Protocol == nil || *wire.Protocol != ProtocolName ||
		wire.ProtocolVersion == nil || *wire.ProtocolVersion != ProtocolVersion {
		return decodedResponse{}, errors.New("unsupported response protocol")
	}
	if wire.OperationID == nil || *wire.OperationID != operationID || wire.Status == nil {
		return decodedResponse{}, errors.New("response does not identify the requested operation")
	}
	if *wire.Status == "error" {
		failure := wire.Failure
		if failure == nil || !nonempty(failure.Code) || !nonempty(failure.Message) || failure.Outcome == nil {
			return decodedResponse{}, errors.New("incomplete error response")
		}
		switch *failure.Outcome {
		case OutcomeNotStarted, OutcomeCommitted, OutcomeUncertain:
		default:
			return decodedResponse{}, errors.New("unknown persistence outcome")
		}
		if wire.ConversationID != nil && *wire.ConversationID == "" {
			return decodedResponse{}, errors.New("empty error conversation identity")
		}
		return decodedResponse{failure: &Error{
			Code: *failure.Code, Outcome: *failure.Outcome, OperationID: operationID,
			Message: *failure.Message, ConversationID: stringValue(wire.ConversationID),
		}}, nil
	}
	if !absolutePath(wire.Workspace) ||
		!sameWorkspace(request.Workspace, *wire.Workspace) {
		return decodedResponse{}, errors.New("response workspace does not match the request")
	}
	if wire.Representation == nil || *wire.Representation != RepresentationText || wire.Warnings == nil {
		return decodedResponse{}, errors.New("missing representation or warnings")
	}
	warnings := make([]Warning, 0, len(*wire.Warnings))
	for _, warning := range *wire.Warnings {
		if !nonempty(warning.Code) || warning.Message == nil {
			return decodedResponse{}, errors.New("incomplete warning")
		}
		warnings = append(warnings, Warning{Code: *warning.Code, Message: *warning.Message})
	}
	if preview {
		if *wire.Status != "planned" || wire.MessagesPlanned == nil || *wire.MessagesPlanned != len(request.Messages) {
			return decodedResponse{}, errors.New("response is not a matching preview")
		}
		return decodedResponse{plan: Plan{
			OperationID: operationID, Workspace: *wire.Workspace, Representation: *wire.Representation,
			MessagesPlanned: *wire.MessagesPlanned, Warnings: warnings,
		}}, nil
	}
	if (*wire.Status != "created" && *wire.Status != "already_exists") ||
		!nonempty(wire.ConversationID) || wire.MessagesWritten == nil || *wire.MessagesWritten != len(request.Messages) {
		return decodedResponse{}, errors.New("response is not a matching import")
	}
	if !absolutePath(wire.DatabasePath) || !absolutePath(wire.TranscriptPath) ||
		samePath(*wire.DatabasePath, *wire.TranscriptPath) {
		return decodedResponse{}, errors.New("response requires distinct absolute database and transcript paths")
	}
	v := wire.Verification
	if v == nil || v.Storage == nil || *v.Storage != "passed" ||
		v.TranscriptRoundTrip == nil || *v.TranscriptRoundTrip != "passed" ||
		v.NativeResume == nil || *v.NativeResume != "not_run" {
		return decodedResponse{}, errors.New("response does not confirm required structural verification")
	}
	return decodedResponse{result: Result{
		OperationID: operationID, ConversationID: *wire.ConversationID, Workspace: *wire.Workspace,
		TranscriptPath: *wire.TranscriptPath, DatabasePath: *wire.DatabasePath,
		MessagesWritten: *wire.MessagesWritten, Status: *wire.Status, Representation: *wire.Representation,
		Warnings: warnings, Verification: Verification{
			Storage: *v.Storage, TranscriptRoundTrip: *v.TranscriptRoundTrip, NativeResume: *v.NativeResume,
		},
	}}, nil
}

func nonempty(s *string) bool { return s != nil && *s != "" }

func absolutePath(s *string) bool {
	return nonempty(s) && strings.IndexByte(*s, 0) < 0 && filepath.IsAbs(*s)
}

func samePath(first, second string) bool {
	if first == second {
		return true
	}
	firstInfo, firstErr := os.Stat(first)
	secondInfo, secondErr := os.Stat(second)
	if firstErr == nil && secondErr == nil {
		return os.SameFile(firstInfo, secondInfo)
	}
	return filepath.Clean(first) == filepath.Clean(second)
}

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func sameWorkspace(requested, returned string) bool {
	if requested == returned {
		return true
	}
	first, firstErr := os.Stat(requested)
	second, secondErr := os.Stat(returned)
	return firstErr == nil && secondErr == nil && first.IsDir() && second.IsDir() && os.SameFile(first, second)
}
