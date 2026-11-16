package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

// maxJSONDepth bounds the recursive walk independently of the output byte limit.
const maxJSONDepth = 128

// validateJSON accepts one complete JSON value. It supplements encoding/json's
// permissive string decoding with Unicode scalar-value and duplicate-key checks.
// Its errors deliberately omit keys and values from the helper's output.
func validateJSON(data []byte) error {
	if !utf8.Valid(data) {
		return errors.New("JSON contains invalid UTF-8")
	}
	if err := validateJSONEscapes(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("JSON contains trailing data or multiple values")
	}
	return nil
}

func validateJSONEscapes(data []byte) error {
	inString := false
	for i := 0; i < len(data); i++ {
		if !inString {
			if data[i] == '"' {
				inString = true
			}
			continue
		}
		switch data[i] {
		case '"':
			inString = false
		case '\\':
			if i+1 >= len(data) {
				return errors.New("JSON contains an invalid string escape")
			}
			if data[i+1] != 'u' {
				switch data[i+1] {
				case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
					i++
					continue
				default:
					return errors.New("JSON contains an invalid string escape")
				}
			}
			value, ok := jsonHex4(data, i+2)
			if !ok {
				return errors.New("JSON contains an invalid Unicode escape")
			}
			switch {
			case value >= 0xd800 && value <= 0xdbff:
				if i+12 > len(data) || data[i+6] != '\\' || data[i+7] != 'u' {
					return errors.New("JSON contains an unpaired surrogate escape")
				}
				low, valid := jsonHex4(data, i+8)
				if !valid || low < 0xdc00 || low > 0xdfff {
					return errors.New("JSON contains an unpaired surrogate escape")
				}
				i += 11
			case value >= 0xdc00 && value <= 0xdfff:
				return errors.New("JSON contains an unpaired surrogate escape")
			default:
				i += 5
			}
		}
	}
	return nil
}

func jsonHex4(data []byte, start int) (uint16, bool) {
	if start+4 > len(data) {
		return 0, false
	}
	var value uint16
	for _, digit := range data[start : start+4] {
		value <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			value |= uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			value |= uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			value |= uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func consumeJSONValue(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return errors.New("invalid JSON value")
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	if depth >= maxJSONDepth {
		return errors.New("JSON nesting limit exceeded")
	}
	switch delimiter {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return errors.New("invalid JSON object")
			}
			key, ok := token.(string)
			if !ok {
				return errors.New("invalid JSON object key")
			}
			if _, found := keys[key]; found {
				return errors.New("JSON contains a duplicate object key")
			}
			keys[key] = struct{}{}
			if err := consumeJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
			return errors.New("invalid JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
			return errors.New("invalid JSON array")
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	return nil
}
