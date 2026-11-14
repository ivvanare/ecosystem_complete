package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// errInvalidJSON is a body that is not one JSON document: malformed, empty, too large, or two
// documents in a row. The second one is refused on purpose — decoding only the first lets a body
// smuggle a payload past whatever validated the shape.
var errInvalidJSON = errors.New("httpapi: the body is not valid JSON")

// unknownFieldError names a field no schema declares.
type unknownFieldError struct{ Field string }

func (e *unknownFieldError) Error() string { return "httpapi: unknown field " + e.Field }

// decodeBody decodes one JSON body with the strictness the contract declares, and says what went
// wrong without writing anything: the two callers map it differently, and one of them ignores it
// on purpose.
func decodeBody(r *http.Request, limit int64, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, limit))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		if field, unknown := unknownField(err); unknown {
			return &unknownFieldError{Field: field}
		}
		return errInvalidJSON
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return errInvalidJSON
	}
	return nil
}

// decodeJSON decodes and answers the request when the body is unusable, reporting whether the
// caller may continue.
//
// openapi.yaml declares `additionalProperties: false` on every request body, so a field the
// schema does not declare is a client bug. Ignoring it silently is the worst of the options: the
// client believes it set something, the server never read it, and the only symptom is a feature
// that "does not work". It is answered as a validation failure naming the field.
func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, target any) bool {
	err := decodeBody(r, limit, target)
	if err == nil {
		return true
	}
	var unknown *unknownFieldError
	if errors.As(err, &unknown) {
		writeValidationError(w, []fieldError{{Field: unknown.Field, Code: "unknown"}})
		return false
	}
	writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
	return false
}

// unknownField reports whether the decoder refused an undeclared field, and which one.
//
// encoding/json has no typed error for this: DisallowUnknownFields produces
// `fmt.Errorf("json: unknown field %q", name)`, so the name is read from the message. The prefix
// is asserted in a test against the real decoder, which is the only thing that keeps this honest
// across Go versions.
func unknownField(err error) (string, bool) {
	const prefix = "json: unknown field "
	message := err.Error()
	if !strings.HasPrefix(message, prefix) {
		return "", false
	}
	name := strings.Trim(strings.TrimPrefix(message, prefix), `"`)
	if name == "" {
		return "", false
	}
	return name, true
}
