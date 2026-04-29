package gocode

import "testing"

func TestGetStateKeyNotFound(t *testing.T) {
	s := &Session{ID: "x"}
	_, err := GetState[string](s, "missing")
	if err == nil {
		t.Error("expected error for missing key, got nil")
	}
}
