package client

import (
	"strings"
	"testing"
)

func TestValidateJSON(t *testing.T) {
	tests := []struct {
		name string
		data string
		ok   bool
	}{
		{"empty object", "{}", true},
		{"surrounding whitespace", " \n\t{\"x\":1}\r\n", true},
		{"array", "[null,true,false,0,1e999,\"text\"]", true},
		{"null", "null", true},
		{"Unicode", "{\"x\":\"blåbær 😀 �\"}", true},
		{"valid surrogate pair", "{\"x\":\"\\ud83d\\ude00\"}", true},
		{"uppercase surrogate pair", "{\"x\":\"\\uD83D\\uDE00\"}", true},
		{"literal backslash u", "{\"x\":\"\\\\uD800\"}", true},
		{"escaped quote", "{\"x\":\"\\\"quoted\\\"\"}", true},
		{"equal keys in separate objects", "[{\"x\":1},{\"x\":2}]", true},
		{"empty", "", false},
		{"only whitespace", " \n\t", false},
		{"multiple objects", "{} {}", false},
		{"multiple scalars", "1 true", false},
		{"trailing junk", "{}x", false},
		{"missing brace", "{\"x\":1", false},
		{"missing value", "{\"x\":}", false},
		{"array trailing comma", "[1,]", false},
		{"object trailing comma", "{\"x\":1,}", false},
		{"NaN", "{\"x\":NaN}", false},
		{"invalid escape", "{\"x\":\"\\q\"}", false},
		{"short Unicode escape", "{\"x\":\"\\u123\"}", false},
		{"nonhex Unicode escape", "{\"x\":\"\\u123z\"}", false},
		{"unescaped newline", "{\"x\":\"a\nb\"}", false},
		{"duplicate key", "{\"secret\":1,\"secret\":2}", false},
		{"nested duplicate key", "{\"x\":[{\"secret\":1,\"secret\":2}]}", false},
		{"escaped duplicate key", "{\"a\":1,\"\\u0061\":2}", false},
		{"surrogate duplicate key", "{\"😀\":1,\"\\ud83d\\ude00\":2}", false},
		{"unpaired high surrogate", "{\"x\":\"\\ud800\"}", false},
		{"unpaired low surrogate", "{\"x\":\"\\udc00\"}", false},
		{"high followed by ordinary text", "{\"x\":\"\\ud800text\"}", false},
		{"high followed by BMP escape", "{\"x\":\"\\ud800\\u0061\"}", false},
		{"high followed by escaped backslash", "{\"x\":\"\\ud800\\\\udc00\"}", false},
		{"high followed by high", "{\"x\":\"\\ud800\\ud800\"}", false},
		{"low followed by high", "{\"x\":\"\\udc00\\ud800\"}", false},
		{"low after pair", "{\"x\":\"\\ud800\\udc00\\udc00\"}", false},
		{"surrogate in key", "{\"\\ud800\":1}", false},
		{"invalid UTF8", string([]byte{'"', 0xff, '"'}), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateJSON([]byte(test.data))
			if (err == nil) != test.ok {
				t.Fatalf("validateJSON() error = %v, want valid = %v", err, test.ok)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("validation error exposed an object key")
			}
		})
	}
}

func TestValidateJSONDepthLimit(t *testing.T) {
	allowed := strings.Repeat("[", maxJSONDepth) + "0" + strings.Repeat("]", maxJSONDepth)
	if err := validateJSON([]byte(allowed)); err != nil {
		t.Fatalf("allowed depth rejected: %v", err)
	}
	tooDeep := "[" + allowed + "]"
	if err := validateJSON([]byte(tooDeep)); err == nil {
		t.Fatal("excessive depth accepted")
	}
}
