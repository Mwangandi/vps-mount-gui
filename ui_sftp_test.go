package main

import (
	"strings"
	"testing"
)

func TestPromptPasswordRequiresWindow(t *testing.T) {
	_, err := promptPassword(nil)
	if err == nil {
		t.Fatal("expected password prompt to require a window")
	}
	if !strings.Contains(err.Error(), "no UI available") {
		t.Fatalf("unexpected error: %v", err)
	}
}
