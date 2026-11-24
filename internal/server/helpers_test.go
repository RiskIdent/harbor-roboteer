package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteProblem(t *testing.T) {
	rec := httptest.NewRecorder()
	writeProblem(rec, http.StatusBadRequest, "body must be valid JSON")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("wrong status code: want %d, got %d", http.StatusBadRequest, rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("wrong content type: want application/problem+json, got %q", got)
	}
	var got problem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v\n%s", err, rec.Body)
	}
	want := problem{Title: "Bad Request", Status: http.StatusBadRequest, Detail: "body must be valid JSON"}
	if got != want {
		t.Errorf("wrong problem\nwant: %#v\ngot:  %#v", want, got)
	}
}
