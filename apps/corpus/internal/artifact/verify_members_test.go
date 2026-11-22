package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyWithMembers(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "file"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := manifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, manifestName), data, 0600); err != nil {
		t.Fatal(err)
	}
	hash, members, err := VerifyWithMembers(dir)
	if err != nil {
		t.Fatal(err)
	}
	oldHash, err := Verify(dir)
	if err != nil || oldHash != hash {
		t.Fatalf("verify hashes differ: %s %s %v", hash, oldHash, err)
	}
	if len(members) != 1 || members["nested/file"] != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("members: %#v", members)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "file"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := VerifyWithMembers(dir); err == nil {
		t.Fatal("accepted corrupt member")
	}
}

func TestReadVerifiedFile(t *testing.T) {
	dir := t.TempDir()
	data := []byte("verified content")
	if err := os.WriteFile(filepath.Join(dir, "file"), data, 0600); err != nil {
		t.Fatal(err)
	}
	manifestData, err := manifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, manifestName), manifestData, 0600); err != nil {
		t.Fatal(err)
	}
	hash, err := Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadVerifiedFile(dir, "file", hash, 64)
	if err != nil || string(got) != string(data) {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err = ReadVerifiedFile(dir, "file", hash, 2); err == nil {
		t.Fatal("accepted oversized file")
	}
	if _, err = ReadVerifiedFile(dir, "file", "wrong", 64); err == nil {
		t.Fatal("accepted wrong manifest hash")
	}
	if err = os.WriteFile(filepath.Join(dir, "file"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadVerifiedFile(dir, "file", hash, 64); err == nil {
		t.Fatal("accepted corrupt file")
	}
}
