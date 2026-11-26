package main

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFakeEnvKeepsOnlyFakeSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.env")
	if err := os.WriteFile(path, []byte("# c\nFAKE_LATENCY_MS=150-500\nSESSION_SECRET=nope\n\nFAKE_DECLINE_RATE=0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readFakeEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"FAKE_LATENCY_MS": "150-500", "FAKE_DECLINE_RATE": "0"}; !maps.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
}
