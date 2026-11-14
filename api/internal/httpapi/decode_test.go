package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// openapi.yaml declares additionalProperties: false, so the strictness these tests pin is the
// contract, not a preference. A field that is silently ignored is a client that believes it set
// something the server never read.

func decodeInto(t *testing.T, body string, limit int64, target any) error {
	t.Helper()
	r := httptest.NewRequest("POST", "/x", strings.NewReader(body))
	return decodeBody(r, limit, target)
}

func TestDecodeAcceptsAWellFormedBody(t *testing.T) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeInto(t, `{"name":"Cena"}`, 4096, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Name != "Cena" {
		t.Fatalf("name = %q", body.Name)
	}
}

func TestDecodeRefusesAnUndeclaredFieldAndNamesIt(t *testing.T) {
	var body struct {
		Name string `json:"name"`
	}
	err := decodeInto(t, `{"name":"Cena","nmae":"typo"}`, 4096, &body)
	var unknown *unknownFieldError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want an unknownFieldError", err)
	}
	if unknown.Field != "nmae" {
		t.Fatalf("field = %q, want nmae", unknown.Field)
	}
}

func TestDecodeRefusesMalformedAndEmptyBodies(t *testing.T) {
	var body struct {
		Name string `json:"name"`
	}
	for _, raw := range []string{``, `{`, `{"name":}`, `not json`} {
		if err := decodeInto(t, raw, 4096, &body); !errors.Is(err, errInvalidJSON) {
			t.Fatalf("body %q: err = %v, want errInvalidJSON", raw, err)
		}
	}
}

// A second document after the first is how a body smuggles a payload past the shape that was
// validated: only the first document is read, so the rest is refused outright.
func TestDecodeRefusesASecondDocument(t *testing.T) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeInto(t, `{"name":"Cena"}{"name":"Otra"}`, 4096, &body); !errors.Is(err, errInvalidJSON) {
		t.Fatalf("err = %v, want errInvalidJSON", err)
	}
}

func TestDecodeRefusesAnOversizedBody(t *testing.T) {
	var body struct {
		Name string `json:"name"`
	}
	raw := `{"name":"` + strings.Repeat("x", 9000) + `"}`
	if err := decodeInto(t, raw, 1024, &body); !errors.Is(err, errInvalidJSON) {
		t.Fatalf("err = %v, want errInvalidJSON", err)
	}
}

// The endpoint level, not the helper: a client typo must be refused by a route, not only by the
// decoder, and the answer must name the field so the client can fix it.
func TestAnEndpointRefusesAnUndeclaredField(t *testing.T) {
	rec := bearerDo(t, backoffice(&fakePlaceAdmin{}, "owner-1", adminScope), http.MethodPut,
		"/v1/admin/places/"+adminPlaceID+"/menu",
		`{"menus":[{"name":"Cena","items":[]}],"menu":[]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"field":"menu"`) {
		t.Fatalf("body = %s, want the field named", rec.Body.String())
	}
}
