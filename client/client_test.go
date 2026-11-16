package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestMain turns the already-built test executable into a deliberately fake
// helper when a test requests it. No shell, compiler subprocess, real agy, or
// native session store is used by the process-contract tests.
func TestMain(m *testing.M) {
	if os.Getenv("AGY_IMPORT_CLIENT_DISCOVERY") == "1" {
		path, err := resolveExecutable("")
		if err != nil {
			os.Exit(95)
		}
		_, _ = io.WriteString(os.Stdout, path)
		os.Exit(0)
	}
	if os.Getenv("AGY_IMPORT_CLIENT_FAKE_HELPER") == "1" {
		os.Exit(fakeHelper())
	}
	os.Exit(m.Run())
}

func fakeHelper() int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 91
	}
	var request struct {
		OperationID string `json:"operation_id"`
		Target      struct {
			Workspace string `json:"workspace"`
		} `json:"target"`
		History struct {
			Messages []Message `json:"messages"`
		} `json:"history"`
	}
	if json.Unmarshal(input, &request) != nil {
		return 92
	}
	if path := os.Getenv("AGY_IMPORT_CLIENT_CAPTURE"); path != "" {
		cwd, _ := os.Getwd()
		capture, _ := json.Marshal(map[string]any{"request": json.RawMessage(input), "args": os.Args[1:], "cwd": cwd})
		if os.WriteFile(path, capture, 0600) != nil {
			return 93
		}
	}
	if path := os.Getenv("AGY_IMPORT_CLIENT_CALLS"); path != "" {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return 94
		}
		_, _ = file.WriteString("call\n")
		_ = file.Close()
	}
	mode := os.Getenv("AGY_IMPORT_CLIENT_MODE")
	if mode == "sleep" {
		time.Sleep(30 * time.Second)
		return 0
	}
	if mode == "stdout-overflow" {
		_, _ = io.WriteString(os.Stdout, strings.Repeat("x", MaxResponseBytes+1))
		time.Sleep(30 * time.Second)
		return 0
	}
	if mode == "large-stderr" || mode == "error-large-stderr" {
		_, _ = io.WriteString(os.Stderr, strings.Repeat("diagnostic-do-not-log", MaxDiagnosticsBytes/4))
	}
	response := map[string]any{
		"protocol": ProtocolName, "protocol_version": ProtocolVersion,
		"operation_id": request.OperationID, "status": "created",
		"conversation_id":  "fake-conversation",
		"workspace":        request.Target.Workspace,
		"database_path":    filepath.Join(request.Target.Workspace, "fake-conversation.db"),
		"transcript_path":  filepath.Join(request.Target.Workspace, "fake-transcript.jsonl"),
		"messages_written": len(request.History.Messages),
		"representation":   RepresentationText,
		"verification":     map[string]any{"storage": "passed", "transcript_round_trip": "passed", "native_resume": "not_run"},
		"warnings":         []any{},
	}
	preview := false
	for _, arg := range os.Args[1:] {
		if arg == "--dry-run" {
			preview = true
		}
	}
	if preview && mode != "force-created-preview" || mode == "planned-for-import" {
		response["status"] = "planned"
		response["messages_planned"] = len(request.History.Messages)
		for _, key := range []string{"conversation_id", "database_path", "transcript_path", "messages_written", "verification"} {
			delete(response, key)
		}
	}
	exit := 0
	switch mode {
	case "error", "error-large-stderr", "error-zero", "error-future-code":
		response = map[string]any{
			"protocol": ProtocolName, "protocol_version": ProtocolVersion,
			"operation_id": request.OperationID, "status": "error",
			"error": map[string]any{"code": CodeUnsupportedTarget, "message": "Synthetic target is unsupported.", "outcome": OutcomeNotStarted},
		}
		if mode == "error-future-code" {
			response["error"].(map[string]any)["code"] = "future_error_code"
		}
		exit = 1
		if mode == "error-zero" {
			exit = 0
		}
	case "bad-error-outcome":
		response["status"] = "error"
		response["error"] = map[string]any{"code": "future", "message": "failure", "outcome": "unknown-new-outcome"}
		exit = 1
	case "malformed":
		_, _ = io.WriteString(os.Stdout, `{"operation_id":`)
		return 0
	case "scalar":
		_, _ = io.WriteString(os.Stdout, `"scalar"`)
		return 0
	case "null":
		_, _ = io.WriteString(os.Stdout, `null`)
		return 0
	case "protocol-mismatch":
		response["protocol_version"] = 2
	case "opid-mismatch":
		response["operation_id"] = "different-operation"
	case "count-mismatch":
		response["messages_written"] = len(request.History.Messages) + 1
	case "workspace-mismatch":
		response["workspace"] = os.Getenv("AGY_IMPORT_CLIENT_OTHER_WORKSPACE")
	case "relative-db":
		response["database_path"] = "relative.db"
	case "same-paths":
		response["transcript_path"] = response["database_path"]
	case "missing-warnings":
		delete(response, "warnings")
	case "null-warnings":
		response["warnings"] = nil
	case "missing-count":
		delete(response, "messages_written")
	case "bad-verification":
		response["verification"].(map[string]any)["native_resume"] = "passed"
	case "bad-warning":
		response["warnings"] = []any{map[string]any{"code": "warning-without-message"}}
	case "additive":
		response["future_field"] = map[string]any{"nested": []any{1, "value", true}}
	case "nonzero-success":
		exit = 9
	case "nonzero-already-exists":
		response["status"] = "already_exists"
		exit = 9
	case "already-exists":
		response["status"] = "already_exists"
	case "lost-response":
		path := os.Getenv("AGY_IMPORT_CLIENT_RECEIPT")
		receipt, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			// This is a synthetic receipt in a test directory, never a native
			// store. The first attempt loses its response after persistence.
			data, _ := json.Marshal(map[string]string{"operation_id": request.OperationID, "conversation_id": "fake-conversation"})
			_, writeErr := receipt.Write(data)
			closeErr := receipt.Close()
			if writeErr != nil || closeErr != nil {
				return 96
			}
			return 17
		}
		if !errors.Is(err, os.ErrExist) {
			return 97
		}
		data, err := os.ReadFile(path)
		var saved map[string]string
		if err != nil || json.Unmarshal(data, &saved) != nil || saved["operation_id"] != request.OperationID {
			return 98
		}
		response["status"] = "already_exists"
		response["conversation_id"] = saved["conversation_id"]
	}
	data, _ := json.Marshal(response)
	switch mode {
	case "duplicate":
		data = append(data[:len(data)-1], []byte(`,"status":"created"}`)...)
	case "invalid-surrogate":
		data = append(data[:len(data)-1], []byte(`,"extra":"\uD800"}`)...)
	case "invalid-utf8":
		data = append(data[:len(data)-1], []byte(`,"extra":"`)...)
		data = append(data, 0xff)
		data = append(data, []byte(`"}`)...)
	}
	_, _ = os.Stdout.Write(data)
	if mode == "two-documents" {
		_, _ = os.Stdout.Write(data)
	}
	return exit
}

func fakeClient(t *testing.T, mode string) (*Client, Request) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGY_IMPORT_CLIENT_FAKE_HELPER", "1")
	t.Setenv("AGY_IMPORT_CLIENT_MODE", mode)
	t.Setenv("AGY_IMPORT_CLIENT_CAPTURE", "")
	t.Setenv("AGY_IMPORT_CLIENT_CALLS", "")
	request := Request{
		Source:    Source{Application: "showagent", Provider: "codex", ConversationID: "source-id", Selection: "last 2"},
		Workspace: t.TempDir(),
		Messages:  []Message{{Role: "user", Text: "Keep Unicode: blåbær, 你好.\n  $(this is historical text)"}, {Role: "assistant", Text: "\n    keep indentation\n"}},
	}
	return New(Options{Executable: executable}), request
}

func TestImportSingleInvocationPreservesSelectedText(t *testing.T) {
	client, request := fakeClient(t, "additive")
	capture := filepath.Join(t.TempDir(), "capture.json")
	calls := filepath.Join(t.TempDir(), "calls.txt")
	t.Setenv("AGY_IMPORT_CLIENT_CAPTURE", capture)
	t.Setenv("AGY_IMPORT_CLIENT_CALLS", calls)
	client.options.Store = filepath.Join(t.TempDir(), "store with spaces")
	result, err := client.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "created" || result.MessagesWritten != 2 || result.OperationID == "" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.TranscriptPath == result.DatabasePath || !strings.HasSuffix(result.TranscriptPath, ".jsonl") {
		t.Fatal("transcript/database distinction lost")
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Request struct {
			OperationID string `json:"operation_id"`
			History     struct {
				Messages []Message `json:"messages"`
			} `json:"history"`
		} `json:"request"`
		Args []string `json:"args"`
		CWD  string   `json:"cwd"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Request.History.Messages, request.Messages) {
		t.Fatal("selected text changed")
	}
	wantArgs := []string{"import", "--input", "-", "--json", "--store", client.options.Store}
	if !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("args = %q; want %q", got.Args, wantArgs)
	}
	cwd, _ := os.Getwd()
	if got.CWD != cwd {
		t.Fatalf("helper cwd = %q; expected original cwd %q", got.CWD, cwd)
	}
	if got.Request.OperationID != result.OperationID {
		t.Fatal("operation identity changed")
	}
	callData, _ := os.ReadFile(calls)
	if string(callData) != "call\n" {
		t.Fatalf("unexpected helper calls: %q", callData)
	}
}

func TestImportExplicitIdentityAndAlreadyExists(t *testing.T) {
	client, request := fakeClient(t, "already-exists")
	request.OperationID = "caller-plan-id"
	result, err := client.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.OperationID != request.OperationID || result.Status != "already_exists" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestPreview(t *testing.T) {
	client, request := fakeClient(t, "")
	plan, err := client.Preview(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Workspace != request.Workspace || plan.MessagesPlanned != len(request.Messages) || plan.OperationID == "" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestStructuredHelperErrors(t *testing.T) {
	for _, mode := range []string{"error", "error-zero", "error-future-code", "error-large-stderr"} {
		t.Run(mode, func(t *testing.T) {
			client, request := fakeClient(t, mode)
			_, err := client.Import(context.Background(), request)
			var problem *Error
			if !errors.As(err, &problem) {
				t.Fatalf("not a typed error: %v", err)
			}
			wantCode := CodeUnsupportedTarget
			if mode == "error-future-code" {
				wantCode = "future_error_code"
			}
			if problem.Code != wantCode || problem.Outcome != OutcomeNotStarted || problem.OperationID == "" {
				t.Fatalf("unexpected error: %#v", problem)
			}
			if len(problem.Diagnostics) > MaxDiagnosticsBytes {
				t.Fatal("unbounded diagnostics")
			}
			if strings.Contains(problem.Error(), "diagnostic-do-not-log") {
				t.Fatal("diagnostics leaked into Error()")
			}
		})
	}
}

func TestInvalidResponsesAreUncertain(t *testing.T) {
	for _, mode := range []string{"malformed", "scalar", "null", "protocol-mismatch", "opid-mismatch", "count-mismatch", "workspace-mismatch", "relative-db", "same-paths", "missing-warnings", "null-warnings", "missing-count", "bad-verification", "bad-warning", "duplicate", "invalid-surrogate", "invalid-utf8", "two-documents", "planned-for-import", "bad-error-outcome"} {
		t.Run(mode, func(t *testing.T) {
			client, request := fakeClient(t, mode)
			t.Setenv("AGY_IMPORT_CLIENT_OTHER_WORKSPACE", t.TempDir())
			_, err := client.Import(context.Background(), request)
			var problem *Error
			if !errors.As(err, &problem) || problem.Code != CodeProtocolError || problem.Outcome != OutcomeUncertain {
				t.Fatalf("unexpected error: %#v (%v)", problem, err)
			}
			if problem.OperationID == "" {
				t.Fatal("lost operation identity")
			}
		})
	}
}

func TestPreviewRejectsImportResult(t *testing.T) {
	client, request := fakeClient(t, "force-created-preview")
	_, err := client.Preview(context.Background(), request)
	var problem *Error
	if !errors.As(err, &problem) || problem.Outcome != OutcomeUncertain {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNonzeroAfterSuccessPreservesIdentityAndInvocationOutcome(t *testing.T) {
	for _, test := range []struct {
		mode, status, outcome string
	}{
		{"nonzero-success", "created", OutcomeCommitted},
		{"nonzero-already-exists", "already_exists", OutcomeUncertain},
	} {
		t.Run(test.status, func(t *testing.T) {
			client, request := fakeClient(t, test.mode)
			result, err := client.Import(context.Background(), request)
			var problem *Error
			if !errors.As(err, &problem) || problem.Code != CodeProtocolError || problem.Outcome != test.outcome {
				t.Fatalf("unexpected error: %v", err)
			}
			if problem.ConversationID != "fake-conversation" || result.ConversationID != problem.ConversationID || result.Status != test.status {
				t.Fatal("lost validated result")
			}
		})
	}
}

func TestLostResponseCanReconcileWithoutDuplicateOrAutomaticRetry(t *testing.T) {
	client, request := fakeClient(t, "lost-response")
	receiptPath := filepath.Join(t.TempDir(), "synthetic-receipt.json")
	callsPath := filepath.Join(t.TempDir(), "calls.txt")
	t.Setenv("AGY_IMPORT_CLIENT_RECEIPT", receiptPath)
	t.Setenv("AGY_IMPORT_CLIENT_CALLS", callsPath)
	_, err := client.Import(context.Background(), request)
	var problem *Error
	if !errors.As(err, &problem) || problem.Outcome != OutcomeUncertain || problem.OperationID == "" {
		t.Fatalf("expected uncertain first attempt with identity: %v", err)
	}
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]string
	if err := json.Unmarshal(before, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["operation_id"] != problem.OperationID {
		t.Fatal("failed call lost the persisted operation identity")
	}
	calls, err := os.ReadFile(callsPath)
	if err != nil || string(calls) != "call\n" {
		t.Fatalf("client retried automatically: %q, %v", calls, err)
	}
	result, err := client.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "already_exists" || result.OperationID != problem.OperationID || result.ConversationID != receipt["conversation_id"] {
		t.Fatalf("retry did not reconcile the original receipt: %#v", result)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("reconciliation changed or duplicated the receipt")
	}
	calls, err = os.ReadFile(callsPath)
	if err != nil || string(calls) != "call\ncall\n" {
		t.Fatalf("retry did not invoke exactly once: %q, %v", calls, err)
	}
}

func TestCancellationKeepsIdentityAndCause(t *testing.T) {
	client, request := fakeClient(t, "sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := client.Import(ctx, request)
	var problem *Error
	if !errors.As(err, &problem) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing deadline cause: %v", err)
	}
	if problem.Outcome != OutcomeUncertain || problem.OperationID == "" {
		t.Fatalf("unexpected error: %#v", problem)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatal("cancellation did not return promptly")
	}
}

func TestAlreadyCanceledDoesNotStartHelper(t *testing.T) {
	client, request := fakeClient(t, "")
	capture := filepath.Join(t.TempDir(), "not-created.json")
	t.Setenv("AGY_IMPORT_CLIENT_CAPTURE", capture)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Import(ctx, request)
	var problem *Error
	if !errors.As(err, &problem) || problem.Outcome != OutcomeNotStarted || !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(capture); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled import started helper")
	}
}

func TestOutputLimitsDoNotDeadlock(t *testing.T) {
	for _, mode := range []string{"large-stderr", "stdout-overflow"} {
		t.Run(mode, func(t *testing.T) {
			client, request := fakeClient(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := client.Import(ctx, request)
			if mode == "large-stderr" && err != nil {
				t.Fatal(err)
			}
			if mode == "stdout-overflow" {
				var problem *Error
				if !errors.As(err, &problem) || problem.Code != CodeProtocolError || problem.Outcome != OutcomeUncertain {
					t.Fatalf("unexpected overflow error: %v", err)
				}
				if errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("overflow only stopped because of outer deadline")
				}
			}
		})
	}
}

func TestMissingHelperAndInvalidRequestDoNotStart(t *testing.T) {
	client, request := fakeClient(t, "")
	client.options.Executable = filepath.Join(t.TempDir(), "missing-helper")
	_, err := client.Import(context.Background(), request)
	var problem *Error
	if !errors.As(err, &problem) || problem.Code != CodeHelperNotFound || problem.Outcome != OutcomeNotStarted || problem.OperationID == "" {
		t.Fatalf("unexpected missing-helper error: %v", err)
	}
	request.Messages = nil
	_, err = client.Import(context.Background(), request)
	if !errors.As(err, &problem) || problem.Code != CodeInvalidRequest || problem.Outcome != OutcomeNotStarted {
		t.Fatalf("unexpected request error: %v", err)
	}
}

func TestMatchingCanonicalWorkspace(t *testing.T) {
	directory := t.TempDir()
	if !sameWorkspace(directory, filepath.Join(directory, ".")) {
		t.Fatal("clean spelling should match")
	}
	if sameWorkspace(directory, t.TempDir()) {
		t.Fatal("different directory should not match")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(directory, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if !sameWorkspace(link, directory) {
		t.Fatal("same directory through symlink should match")
	}
	// Cleaning before resolving a symlink changes the meaning of link/...
	// Stat must resolve the original spelling, not a lexically cleaned path.
	withParent := link + string(filepath.Separator) + ".."
	if sameWorkspace(withParent, filepath.Dir(link)) {
		t.Fatal("lexically equal paths through symlink/.. identify different directories")
	}
}

func TestResponseMemberNamesAreCaseSensitive(t *testing.T) {
	request := Request{Workspace: t.TempDir(), Messages: []Message{{Role: "user", Text: "content"}}}
	const operationID = "response-case-test"
	newResponse := func() map[string]any {
		return map[string]any{
			"protocol": ProtocolName, "protocol_version": ProtocolVersion,
			"operation_id": operationID, "status": "created", "conversation_id": "test-conversation",
			"workspace": request.Workspace, "database_path": filepath.Join(request.Workspace, "db"),
			"transcript_path": filepath.Join(request.Workspace, "transcript"), "messages_written": 1,
			"representation": RepresentationText, "warnings": []any{map[string]any{"code": "warning", "message": ""}},
			"verification": map[string]any{"storage": "passed", "transcript_round_trip": "passed", "native_resume": "not_run"},
		}
	}
	check := func(t *testing.T, response map[string]any, wantError bool) {
		t.Helper()
		data, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		_, err = decodeResponse(data, operationID, request, false)
		if (err != nil) != wantError {
			t.Fatalf("decode error = %v; expected error = %v", err, wantError)
		}
	}
	for _, key := range []string{"protocol", "protocol_version", "operation_id", "status", "conversation_id", "workspace", "database_path", "transcript_path", "messages_written", "representation", "warnings", "verification"} {
		t.Run(key, func(t *testing.T) {
			response := newResponse()
			response[strings.ToUpper(key)] = response[key]
			delete(response, key)
			check(t, response, true)
		})
	}
	for _, key := range []string{"storage", "transcript_round_trip", "native_resume"} {
		t.Run("verification/"+key, func(t *testing.T) {
			response := newResponse()
			verification := response["verification"].(map[string]any)
			verification[strings.ToUpper(key)] = verification[key]
			delete(verification, key)
			check(t, response, true)
		})
	}
	for _, key := range []string{"code", "message"} {
		t.Run("warning/"+key, func(t *testing.T) {
			response := newResponse()
			warning := response["warnings"].([]any)[0].(map[string]any)
			warning[strings.ToUpper(key)] = warning[key]
			delete(warning, key)
			check(t, response, true)
		})
	}
	for _, key := range []string{"code", "message", "outcome"} {
		t.Run("error/"+key, func(t *testing.T) {
			response := newResponse()
			response["status"] = "error"
			failure := map[string]any{"code": "target_busy", "message": "busy", "outcome": OutcomeNotStarted}
			failure[strings.ToUpper(key)] = failure[key]
			delete(failure, key)
			response["error"] = failure
			check(t, response, true)
		})
	}
	t.Run("additive case variants do not overwrite", func(t *testing.T) {
		response := newResponse()
		response["Protocol"] = "another-protocol"
		response["Status"] = "error"
		response["verification"].(map[string]any)["Storage"] = "failed"
		response["warnings"].([]any)[0].(map[string]any)["Code"] = ""
		check(t, response, false)
	})
	t.Run("inactive variant members remain additive", func(t *testing.T) {
		response := newResponse()
		response["error"] = 42
		response["messages_planned"] = "future-value"
		check(t, response, false)
	})
	t.Run("optional error conversation identity cannot be null", func(t *testing.T) {
		response := newResponse()
		response["status"] = "error"
		response["conversation_id"] = nil
		response["error"] = map[string]any{"code": "target_busy", "message": "busy", "outcome": OutcomeNotStarted}
		check(t, response, true)
	})
}

func TestResponseRejectsNULPathsAndSameFileAliases(t *testing.T) {
	request := Request{Workspace: t.TempDir(), Messages: []Message{{Role: "user", Text: "content"}}}
	response := map[string]any{
		"protocol": ProtocolName, "protocol_version": ProtocolVersion,
		"operation_id": "path-test", "status": "created", "conversation_id": "test-conversation",
		"workspace": request.Workspace, "database_path": filepath.Join(request.Workspace, "db"),
		"transcript_path": filepath.Join(request.Workspace, "transcript"), "messages_written": 1,
		"representation": RepresentationText, "warnings": []any{},
		"verification": map[string]any{"storage": "passed", "transcript_round_trip": "passed", "native_resume": "not_run"},
	}
	for _, field := range []string{"workspace", "database_path", "transcript_path"} {
		t.Run("NUL/"+field, func(t *testing.T) {
			original := response[field]
			response[field] = original.(string) + "\x00suffix"
			data, _ := json.Marshal(response)
			_, err := decodeResponse(data, "path-test", request, false)
			response[field] = original
			if err == nil {
				t.Fatal("NUL path accepted")
			}
		})
	}
	database := response["database_path"].(string)
	if err := os.WriteFile(database, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(request.Workspace, "alias")
	if err := os.Link(database, alias); err != nil {
		t.Skipf("hard link unavailable: %v", err)
	}
	response["transcript_path"] = alias
	data, _ := json.Marshal(response)
	if _, err := decodeResponse(data, "path-test", request, false); err == nil {
		t.Fatal("same underlying database/transcript file accepted")
	}
}

func TestBoundedCaptureAcknowledgesAndLimits(t *testing.T) {
	triggered := 0
	capture := &boundedCapture{limit: 3, overflow: func() { triggered++ }}
	for _, data := range []string{"ab", "cd", "ef"} {
		n, err := capture.Write([]byte(data))
		if n != len(data) || err != nil {
			t.Fatal("short write")
		}
	}
	got, overflow := capture.snapshot()
	if !bytes.Equal(got, []byte("abc")) || !overflow || triggered != 1 {
		t.Fatalf("got %q overflow=%v callbacks=%d", got, overflow, triggered)
	}
}
