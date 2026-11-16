package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// RequestDigest returns the lowercase SHA-256 of the protocol-defined semantic
// request. OperationID is excluded. It validates portable content and message
// limits, but does not check host-specific path rules, filesystem existence or
// the final JSON envelope size; Import and Preview perform their local checks.
// No Unicode, whitespace or path normalization is applied.
func RequestDigest(request Request) (string, error) {
	if err := validateContent(request); err != nil {
		return "", err
	}
	return digestRequest(request), nil
}

type wireRequest struct {
	Protocol        string      `json:"protocol"`
	ProtocolVersion int         `json:"protocol_version"`
	OperationID     string      `json:"operation_id"`
	Source          *Source     `json:"source,omitempty"`
	Target          wireTarget  `json:"target"`
	History         wireHistory `json:"history"`
	Policy          *wirePolicy `json:"policy,omitempty"`
}

type wireTarget struct {
	Workspace string `json:"workspace"`
	Title     string `json:"title,omitempty"`
}

type wireHistory struct {
	Representation string    `json:"representation"`
	Truncated      bool      `json:"truncated"`
	Redacted       bool      `json:"redacted"`
	Messages       []Message `json:"messages"`
}

type wirePolicy struct {
	AllowTruncatedHistory bool `json:"allow_truncated_history"`
}

func prepareRequest(request Request) (operationID string, data []byte, err error) {
	operationID = request.OperationID
	if operationID != "" && !validOperationID(operationID) {
		return "", nil, requestError(CodeInvalidRequest, "", "operation ID has an invalid format")
	}
	digest, err := RequestDigest(request)
	if err != nil {
		if detail, ok := err.(*Error); ok {
			detail.OperationID = operationID
		}
		return operationID, nil, err
	}
	if operationID == "" {
		operationID = "sha256:" + digest
	}
	if !filepath.IsAbs(request.Workspace) {
		return operationID, nil, requestError(CodeWorkspaceInvalid, operationID, "destination workspace must be an absolute local path")
	}
	wire := makeWireRequest(request, operationID)
	// encoding/json builds an encoded value before writing it. Check its exact
	// size first so heavily escaped content cannot allocate an oversized envelope.
	if wireRequestSize(wire) > MaxRequestBytes {
		return operationID, nil, requestError(CodeInvalidRequest, operationID, "encoded request exceeds the 64 MiB limit")
	}
	data, err = json.Marshal(wire)
	if err != nil {
		return operationID, nil, &Error{
			Code: CodeInvalidRequest, Outcome: OutcomeNotStarted,
			OperationID: operationID, Message: "could not encode import request", Cause: err,
		}
	}
	if len(data) > MaxRequestBytes {
		return operationID, nil, requestError(CodeInvalidRequest, operationID, "encoded request exceeds the 64 MiB limit")
	}
	return operationID, data, nil
}

func requestError(code, operationID, message string) *Error {
	return &Error{Code: code, Outcome: OutcomeNotStarted, OperationID: operationID, Message: message}
}

func validOperationID(value string) bool {
	if len(value) < 1 || len(value) > 128 || !asciiAlphanumeric(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if !asciiAlphanumeric(c) && c != '.' && c != '_' && c != ':' && c != '-' {
			return false
		}
	}
	return true
}

func asciiAlphanumeric(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func validateContent(request Request) error {
	if request.Workspace == "" || strings.IndexByte(request.Workspace, 0) >= 0 {
		return requestError(CodeWorkspaceInvalid, "", "destination workspace is empty or contains a NUL byte")
	}
	if len(request.Messages) == 0 || len(request.Messages) > MaxMessages {
		return requestError(CodeInvalidRequest, "", "message count must be between 1 and 100000")
	}
	if request.Truncated && !request.AllowTruncatedHistory {
		return requestError(CodeIncompleteHistory, "", "truncated history requires explicit acceptance")
	}
	// This lower bound rejects enormous source metadata before hashing/encoding.
	// JSON syntax and escaping are checked separately against the exact size.
	rawBytes := 0
	checkString := func(value string) error {
		if len(value) > MaxRequestBytes-rawBytes {
			return requestError(CodeInvalidRequest, "", "request content exceeds the 64 MiB limit")
		}
		rawBytes += len(value)
		if !utf8.ValidString(value) {
			return requestError(CodeInvalidRequest, "", "request contains invalid UTF-8")
		}
		return nil
	}
	for _, value := range []string{
		request.Source.Application, request.Source.Provider,
		request.Source.ConversationID, request.Source.Workspace,
		request.Source.Selection, request.Source.Revision,
		request.Workspace, request.Title,
	} {
		if err := checkString(value); err != nil {
			return err
		}
	}
	for _, message := range request.Messages {
		if message.Role != "user" && message.Role != "assistant" {
			return requestError(CodeInvalidRequest, "", "message role must be user or assistant")
		}
		if len(message.Text) == 0 || len(message.Text) > MaxMessageBytes {
			return requestError(CodeInvalidRequest, "", "message text must contain between 1 byte and 16 MiB")
		}
		if err := checkString(message.Role); err != nil {
			return err
		}
		if err := checkString(message.Text); err != nil {
			return err
		}
	}
	return nil
}

func digestRequest(request Request) string {
	digest := sha256.New()
	writeDigestToken(digest, "agy-import:request:1")
	for _, token := range []string{
		request.Source.Application, request.Source.Provider,
		request.Source.ConversationID, request.Source.Workspace,
		request.Source.Selection, request.Source.Revision,
		request.Workspace, request.Title, RepresentationText,
		boolToken(request.Truncated), boolToken(request.Redacted),
		strconv.Itoa(len(request.Messages)),
	} {
		writeDigestToken(digest, token)
	}
	for _, message := range request.Messages {
		writeDigestToken(digest, message.Role)
		writeDigestToken(digest, message.Text)
	}
	writeDigestToken(digest, boolToken(request.AllowTruncatedHistory))
	return hex.EncodeToString(digest.Sum(nil))
}

func writeDigestToken(digest hash.Hash, value string) {
	_, _ = digest.Write([]byte(strconv.Itoa(len(value))))
	_, _ = digest.Write([]byte{':'})
	_, _ = digest.Write([]byte(value))
}

func boolToken(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func makeWireRequest(request Request, operationID string) wireRequest {
	wire := wireRequest{
		Protocol: ProtocolName, ProtocolVersion: ProtocolVersion, OperationID: operationID,
		Target: wireTarget{Workspace: request.Workspace, Title: request.Title},
		History: wireHistory{
			Representation: RepresentationText, Truncated: request.Truncated,
			Redacted: request.Redacted, Messages: request.Messages,
		},
	}
	if request.Source != (Source{}) {
		source := request.Source
		wire.Source = &source
	}
	if request.AllowTruncatedHistory {
		wire.Policy = &wirePolicy{AllowTruncatedHistory: true}
	}
	return wire
}

// Size helpers mirror encoding/json's string escaping, including HTML-sensitive
// characters and U+2028/U+2029. Sums saturate just above the protocol limit.
type sizedField struct {
	name string
	size int
}

func wireRequestSize(wire wireRequest) int {
	fields := []sizedField{
		{"protocol", jsonStringSize(wire.Protocol)},
		{"protocol_version", 1},
		{"operation_id", jsonStringSize(wire.OperationID)},
	}
	if wire.Source != nil {
		var source []sizedField
		for _, field := range []struct{ name, value string }{
			{"application", wire.Source.Application}, {"provider", wire.Source.Provider},
			{"conversation_id", wire.Source.ConversationID}, {"workspace", wire.Source.Workspace},
			{"selection", wire.Source.Selection}, {"revision", wire.Source.Revision},
		} {
			if field.value != "" {
				source = append(source, sizedField{field.name, jsonStringSize(field.value)})
			}
		}
		fields = append(fields, sizedField{"source", jsonObjectSize(source)})
	}
	target := []sizedField{{"workspace", jsonStringSize(wire.Target.Workspace)}}
	if wire.Target.Title != "" {
		target = append(target, sizedField{"title", jsonStringSize(wire.Target.Title)})
	}
	fields = append(fields, sizedField{"target", jsonObjectSize(target)})
	messages := 2 // []
	for index, message := range wire.History.Messages {
		if index > 0 {
			messages = sizeSum(messages, 1)
		}
		messages = sizeSum(messages, jsonObjectSize([]sizedField{
			{"role", jsonStringSize(message.Role)}, {"text", jsonStringSize(message.Text)},
		}))
		if messages > MaxRequestBytes {
			return messages
		}
	}
	fields = append(fields, sizedField{"history", jsonObjectSize([]sizedField{
		{"representation", jsonStringSize(wire.History.Representation)},
		{"truncated", jsonBoolSize(wire.History.Truncated)},
		{"redacted", jsonBoolSize(wire.History.Redacted)},
		{"messages", messages},
	})})
	if wire.Policy != nil {
		fields = append(fields, sizedField{"policy", jsonObjectSize([]sizedField{
			{"allow_truncated_history", jsonBoolSize(wire.Policy.AllowTruncatedHistory)},
		})})
	}
	return jsonObjectSize(fields)
}

func jsonObjectSize(fields []sizedField) int {
	size := 2 // {}
	for index, field := range fields {
		if index > 0 {
			size = sizeSum(size, 1)
		}
		size = sizeSum(size, jsonStringSize(field.name))
		size = sizeSum(size, 1) // :
		size = sizeSum(size, field.size)
	}
	return size
}

func jsonBoolSize(value bool) int {
	if value {
		return 4
	}
	return 5
}

func jsonStringSize(value string) int {
	size := 2 // quotes
	for _, r := range value {
		var n int
		switch r {
		case '"', '\\', '\b', '\f', '\n', '\r', '\t':
			n = 2
		case '<', '>', '&', '\u2028', '\u2029':
			n = 6
		default:
			if r < 0x20 {
				n = 6
			} else {
				n = utf8.RuneLen(r)
			}
		}
		size = sizeSum(size, n)
		if size > MaxRequestBytes {
			return size
		}
	}
	return size
}

func sizeSum(a, b int) int {
	if a > MaxRequestBytes || b > MaxRequestBytes-a {
		return MaxRequestBytes + 1
	}
	return a + b
}
