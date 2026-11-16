package client

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleRequest(t *testing.T) Request {
	t.Helper()
	return Request{
		Workspace: t.TempDir(),
		Messages: []Message{
			{Role: "user", Text: "Keep Unicode: blåbær and 🐦."},
			{Role: "assistant", Text: "  preserve whitespace\r\n\n    preserve indentation  "},
		},
	}
}

func fixtureRequest(t *testing.T) Request {
	t.Helper()
	data, err := os.ReadFile("testdata/import-request.json")
	if err != nil {
		t.Fatal(err)
	}
	var wire wireRequest
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	request := Request{
		OperationID: wire.OperationID,
		Workspace:   wire.Target.Workspace, Title: wire.Target.Title,
		Messages:  wire.History.Messages,
		Truncated: wire.History.Truncated, Redacted: wire.History.Redacted,
	}
	if wire.Source != nil {
		request.Source = *wire.Source
	}
	if wire.Policy != nil {
		request.AllowTruncatedHistory = wire.Policy.AllowTruncatedHistory
	}
	return request
}

func TestRequestDigestFixture(t *testing.T) {
	request := fixtureRequest(t)
	data, err := os.ReadFile("testdata/request-digest.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		RequestDigest      string `json:"request_digest"`
		DerivedOperationID string `json:"derived_operation_id"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	digest, err := RequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	if digest != fixture.RequestDigest {
		t.Fatalf("digest = %s; want %s", digest, fixture.RequestDigest)
	}
	if "sha256:"+digest != fixture.DerivedOperationID {
		t.Fatal("derived identity differs from published fixture")
	}
	request.OperationID = "a-different-explicit-operation"
	other, err := RequestDigest(request)
	if err != nil || other != digest {
		t.Fatalf("operation ID changed semantic digest: %s, %v", other, err)
	}
}

func TestPrepareRequestPreservesTextAndIdentity(t *testing.T) {
	request := sampleRequest(t)
	request.Source = Source{
		Application: "showagent", Provider: "codex", ConversationID: "source-id",
		Workspace: `C:\source\workspace`, Selection: "last:2",
	}
	id, data, err := prepareRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := RequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	if id != "sha256:"+digest {
		t.Fatalf("unexpected derived operation ID %q", id)
	}
	var wire wireRequest
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Protocol != ProtocolName || wire.ProtocolVersion != ProtocolVersion || wire.OperationID != id {
		t.Fatal("request envelope is incorrect")
	}
	if wire.Source == nil || *wire.Source != request.Source {
		t.Fatal("source provenance changed")
	}
	if wire.History.Representation != RepresentationText || wire.History.Truncated || wire.History.Redacted {
		t.Fatal("unexpected default history flags")
	}
	if len(wire.History.Messages) != len(request.Messages) {
		t.Fatal("message count changed")
	}
	for i := range request.Messages {
		if wire.History.Messages[i] != request.Messages[i] {
			t.Fatalf("message %d changed", i)
		}
	}
	request.OperationID = "caller-plan-001"
	id, _, err = prepareRequest(request)
	if err != nil || id != request.OperationID {
		t.Fatalf("caller operation ID not retained: %q, %v", id, err)
	}
}

func TestPrepareRequestOptionalFields(t *testing.T) {
	request := sampleRequest(t)
	_, data, err := prepareRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"source", "policy"} {
		if _, present := document[key]; present {
			t.Fatalf("empty optional %s was encoded", key)
		}
	}
	request.Truncated = true
	request.Redacted = true
	request.AllowTruncatedHistory = true
	_, data, err = prepareRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	var wire wireRequest
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Policy == nil || !wire.Policy.AllowTruncatedHistory || !wire.History.Truncated || !wire.History.Redacted {
		t.Fatal("explicit fidelity/policy flags were not preserved")
	}
}

func TestPrepareRequestRejectsInvalidContent(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Request)
		code string
	}{
		{"missing_workspace", func(r *Request) { r.Workspace = "" }, CodeWorkspaceInvalid},
		{"relative_workspace", func(r *Request) { r.Workspace = "relative/work" }, CodeWorkspaceInvalid},
		{"nul_workspace", func(r *Request) { r.Workspace += "\x00hidden" }, CodeWorkspaceInvalid},
		{"no_messages", func(r *Request) { r.Messages = nil }, CodeInvalidRequest},
		{"unsupported_role", func(r *Request) { r.Messages[0].Role = "system" }, CodeInvalidRequest},
		{"empty_text", func(r *Request) { r.Messages[0].Text = "" }, CodeInvalidRequest},
		{"invalid_text_utf8", func(r *Request) { r.Messages[0].Text = "secret\xff" }, CodeInvalidRequest},
		{"invalid_source_utf8", func(r *Request) { r.Source.Revision = "secret\xff" }, CodeInvalidRequest},
		{"invalid_title_utf8", func(r *Request) { r.Title = "secret\xff" }, CodeInvalidRequest},
		{"truncated_without_acceptance", func(r *Request) { r.Truncated = true }, CodeIncompleteHistory},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := sampleRequest(t)
			request.OperationID = "known-operation"
			test.edit(&request)
			id, data, err := prepareRequest(request)
			var detail *Error
			if !errors.As(err, &detail) {
				t.Fatalf("expected typed validation error, got %v", err)
			}
			if detail.Code != test.code || detail.Outcome != OutcomeNotStarted {
				t.Fatalf("unexpected error: %+v", detail)
			}
			if id != request.OperationID || detail.OperationID != id {
				t.Fatal("validation lost known operation identity")
			}
			if data != nil || strings.Contains(err.Error(), "secret") {
				t.Fatal("invalid request returned data or exposed content")
			}
		})
	}
}

func TestOperationIDValidation(t *testing.T) {
	for _, value := range []string{"a", "Plan-1.2_3:4", "sha256:" + strings.Repeat("0", 64), strings.Repeat("a", 128)} {
		if !validOperationID(value) {
			t.Fatalf("valid operation ID rejected: %q", value)
		}
	}
	for _, value := range []string{"", "-leading", ".leading", "has space", "trailing\n", "x/y", "blåbær", strings.Repeat("a", 129)} {
		if validOperationID(value) {
			t.Fatalf("invalid operation ID accepted: %q", value)
		}
	}
	request := sampleRequest(t)
	request.OperationID = "private\nvalue"
	id, _, err := prepareRequest(request)
	var detail *Error
	if !errors.As(err, &detail) || id != "" || detail.OperationID != "" {
		t.Fatal("invalid operation ID must not be retained")
	}
	if strings.Contains(err.Error(), "private") {
		t.Fatal("invalid operation ID leaked into error text")
	}
}

func TestRequestDigestSensitiveToSemanticChanges(t *testing.T) {
	base := sampleRequest(t)
	base.Source = Source{Application: "showagent", Provider: "codex", ConversationID: "source"}
	want, err := RequestDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	edits := []func(*Request){
		func(r *Request) { r.Source.Application += "2" },
		func(r *Request) { r.Source.Provider = "claude" },
		func(r *Request) { r.Source.ConversationID += "2" },
		func(r *Request) { r.Source.Workspace = "original" },
		func(r *Request) { r.Source.Selection = "last:1" },
		func(r *Request) { r.Source.Revision = "revision" },
		func(r *Request) { r.Workspace += "2" },
		func(r *Request) { r.Title = "title" },
		func(r *Request) { r.Redacted = true },
		func(r *Request) { r.AllowTruncatedHistory = true },
		func(r *Request) { r.Truncated = true; r.AllowTruncatedHistory = true },
		func(r *Request) { r.Messages[0].Role = "assistant" },
		func(r *Request) { r.Messages[0].Text += " " },
		func(r *Request) { r.Messages[0], r.Messages[1] = r.Messages[1], r.Messages[0] },
		func(r *Request) { r.Messages = r.Messages[:1] },
	}
	for i, edit := range edits {
		request := base
		request.Messages = append([]Message(nil), base.Messages...)
		edit(&request)
		got, err := RequestDigest(request)
		if err != nil || got == want {
			t.Fatalf("semantic edit %d did not change digest: %s, %v", i, got, err)
		}
	}
}

func TestRequestDigestDoesNotNormalizeText(t *testing.T) {
	request := sampleRequest(t)
	request.Messages = []Message{{Role: "user", Text: "é\r\n"}}
	first, _ := RequestDigest(request)
	request.Messages[0].Text = "e\u0301\r\n"
	second, _ := RequestDigest(request)
	request.Messages[0].Text = "é\n"
	third, _ := RequestDigest(request)
	if first == second || first == third || second == third {
		t.Fatal("Unicode normalization or line endings were lost in identity")
	}
}

func TestPrepareRequestLeavesFilesystemChecksToHelper(t *testing.T) {
	request := sampleRequest(t)
	request.Workspace = filepath.Join(request.Workspace, "not-created")
	if _, _, err := prepareRequest(request); err != nil {
		t.Fatalf("absolute path was rejected before helper filesystem validation: %v", err)
	}
}

func TestRequestLimits(t *testing.T) {
	t.Run("message_count", func(t *testing.T) {
		request := sampleRequest(t)
		request.Messages = make([]Message, MaxMessages+1)
		if _, _, err := prepareRequest(request); err == nil {
			t.Fatal("too many messages accepted")
		}
	})
	t.Run("message_bytes", func(t *testing.T) {
		request := sampleRequest(t)
		request.Messages = []Message{{Role: "user", Text: strings.Repeat("x", MaxMessageBytes+1)}}
		if _, _, err := prepareRequest(request); err == nil {
			t.Fatal("oversized message accepted")
		}
	})
	t.Run("total_raw_bytes", func(t *testing.T) {
		request := sampleRequest(t)
		text := strings.Repeat("x", MaxMessageBytes)
		request.Messages = []Message{{Role: "user", Text: text}, {Role: "assistant", Text: text}, {Role: "user", Text: text}, {Role: "assistant", Text: text}}
		if _, _, err := prepareRequest(request); err == nil {
			t.Fatal("oversized request accepted")
		}
	})
	t.Run("escaped_json_bytes", func(t *testing.T) {
		request := sampleRequest(t)
		// Each NUL is six encoded bytes, despite being one raw UTF-8 byte.
		request.Messages = []Message{{Role: "user", Text: strings.Repeat("\x00", MaxRequestBytes/6+1)}}
		id, data, err := prepareRequest(request)
		var detail *Error
		if !errors.As(err, &detail) || detail.Code != CodeInvalidRequest || data != nil || id == "" {
			t.Fatalf("escaped-size limit not enforced with identity: %q, %v", id, err)
		}
	})
}

func TestEncodedSizeMatchesJSON(t *testing.T) {
	random := rand.New(rand.NewSource(76))
	alphabet := []rune("abc<>/&\\\"\b\f\n\r\t\x00\x1f blåbær🐦\u2028\u2029")
	for iteration := 0; iteration < 100; iteration++ {
		request := sampleRequest(t)
		var text strings.Builder
		for n := random.Intn(100); n > 0; n-- {
			text.WriteRune(alphabet[random.Intn(len(alphabet))])
		}
		request.Title = text.String()
		request.Source = Source{Application: "showagent", Revision: text.String()}
		request.Redacted = iteration%2 == 0
		request.AllowTruncatedHistory = iteration%3 == 0
		request.Truncated = request.AllowTruncatedHistory
		request.Messages[0].Text += text.String()
		wire := makeWireRequest(request, "size-test")
		encoded, err := json.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		if size := wireRequestSize(wire); size != len(encoded) {
			t.Fatalf("iteration %d: calculated %d bytes, JSON produced %d", iteration, size, len(encoded))
		}
	}
}

func TestErrorPreservesCauseWithoutPrintingDiagnostics(t *testing.T) {
	err := &Error{
		Code: CodeOutcomeUncertain, Outcome: OutcomeUncertain, OperationID: "same-id",
		Message: "helper invocation was canceled", Cause: context.Canceled,
		Diagnostics: "private transcript text",
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation cause was lost")
	}
	if strings.Contains(err.Error(), err.Diagnostics) {
		t.Fatal("diagnostics leaked into routine error text")
	}
}
