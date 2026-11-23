package classify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackVerifierRejectsInvalidFiles(t *testing.T) {
	for _, tc := range []struct{ name, file, want string }{
		{"changed input", "input.jsonl", "input.jsonl"},
		{"changed result", "result", ".capture"},
		{"symlink", "state.json", "state.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, selection, dir, articles := packFixture(t, false, false)
			name := tc.file
			switch name {
			case "result":
				var state openAITrialState
				if found, err := readTrialState(filepath.Join(dir, "state.json"), &state); err != nil || !found || state.Output == nil {
					t.Fatal("missing output binding", err)
				}
				name = state.Output.Capture
				packWriteTest(t, dir, name, []byte("changed"))
			case "state.json":
				path := filepath.Join(dir, name)
				if err := os.Rename(path, path+".bak"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".bak", path); err != nil {
					t.Fatal(err)
				}
			default:
				packWriteTest(t, dir, name, []byte("changed"))
			}
			if _, err := verifyPackPair(dir, articles, selection); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("invalid %s accepted: %v", name, err)
			}
		})
	}
}
