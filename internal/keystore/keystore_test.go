package keystore

import (
	"testing"
)

func TestSetGetRemove(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := Set("acme", "sk-secret-1234567890"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := Get("acme")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "sk-secret-1234567890" {
		t.Fatalf("Get = %q, want the stored key", got)
	}

	keys, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("List len = %d, want 1", len(keys))
	}

	if err := Remove("acme"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got, _ = Get("acme")
	if got != "" {
		t.Fatalf("after Remove, Get = %q, want empty", got)
	}
}

func TestMask(t *testing.T) {
	if got := Mask("short"); got != "****" {
		t.Errorf("Mask(short) = %q, want ****", got)
	}
	if got := Mask("sk-1234567890abcd"); got != "sk-1****abcd" {
		t.Errorf("Mask = %q, want sk-1****abcd", got)
	}
}
