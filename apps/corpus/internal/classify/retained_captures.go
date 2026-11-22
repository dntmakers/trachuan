package classify

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (r *Runner) inspectCaptures(intents map[string]Submission, store *retainedStore) error {
	root := filepath.Join(r.IntentDir, "evidence")
	owners, err := readRetainedDirectory(root)
	if err != nil {
		return err
	}
	for _, owner := range owners {
		dir := filepath.Join(root, owner.Name())
		if _, ok := intents[owner.Name()]; !ok || !owner.IsDir() {
			return fmt.Errorf("unowned classification evidence %s", dir)
		}
		scopes, readErr := readRetainedDirectory(dir)
		if readErr != nil {
			return readErr
		}
		for _, scope := range scopes {
			path := filepath.Join(dir, scope.Name())
			if scope.IsDir() && scope.Name() == "responses" {
				if err := inspectProviderResponses(path, owner.Name()); err != nil {
					return err
				}
				continue
			}
			if !scope.IsDir() || (scope.Name() != "list" && !validKey(scope.Name())) {
				return fmt.Errorf("invalid classification evidence scope %s", path)
			}
			captures, listErr := readRetainedDirectory(path)
			if listErr != nil {
				return listErr
			}
			for _, c := range captures {
				captureDir := filepath.Join(path, c.Name())
				if !c.IsDir() || !validID(c.Name()) {
					return fmt.Errorf("invalid classification evidence capture %s", captureDir)
				}
				if err := inspectCapture(r.IntentDir, captureDir, owner.Name(), scope.Name(), c.Name(), intents, store); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func inspectCapture(root, dir, owner, scope, id string, intents map[string]Submission, store *retainedStore) error {
	files, err := readRetainedDirectory(dir)
	if err != nil {
		return err
	}
	var start captureStart
	startPath := filepath.Join(dir, "start.json")
	if err = readRetainedFile(startPath, &start); err != nil {
		return err
	}
	if start.Owner != owner || start.ID != id || captureScope(start.BatchID) != scope ||
		(scope == "list" && (start.CreatedAt != nil || start.After == nil || start.Before == nil || start.Before.Before(*start.After))) ||
		(scope != "list" && (start.After != nil || start.Before != nil)) {
		return fmt.Errorf("invalid classification evidence identity %s", startPath)
	}
	n := 0
	var end *captureEnd
	for _, f := range files {
		path := filepath.Join(dir, f.Name())
		if !f.Type().IsRegular() {
			return fmt.Errorf("invalid classification evidence file %s", path)
		}
		switch f.Name() {
		case "start.json":
		case "end.json":
			end = new(captureEnd)
			if err = readRetainedFile(path, end); err != nil {
				return err
			}
		default:
			if f.Name() != captureRecordName(n) {
				return fmt.Errorf("unexpected classification evidence record %s", path)
			}
			n++
			if scope == "list" {
				var raw string
				if err = readRetainedFile(path, &raw); err != nil {
					return err
				}
			} else {
				var item capturedResult
				if err = readRetainedFile(path, &item); err != nil {
					return err
				}
				if _, err = verifyCapturedLine(item); err != nil {
					return fmt.Errorf("invalid captured result %s: %w", path, err)
				}
				claim, _, _ := strings.Cut(item.CustomID, "-")
				if _, ok := intents[claim]; ok {
					store.retryAbandoned[claim] = false
					store.claimed[claim] = true
				}
			}
		}
	}
	if end != nil && end.Records != n {
		return fmt.Errorf("classification evidence record count differs from %s", filepath.Join(dir, "end.json"))
	}
	if start.BatchID != "" && n > 0 {
		var at time.Time
		if start.CreatedAt != nil {
			at = *start.CreatedAt
		}
		c := storedCapture{dir: dir, records: n, createdAt: at}
		if end != nil && end.Error == "" {
			c.proof, err = captureProof(root, dir, files)
			if err != nil {
				return err
			}
		}
		store.downloads[start.BatchID] = append(store.downloads[start.BatchID], c)
	}
	return nil
}

func captureProof(root, dir string, files []fs.DirEntry) (captureReference, error) {
	path, err := filepath.Rel(root, dir)
	if err != nil {
		return captureReference{}, fmt.Errorf("resolve capture path: %w", err)
	}
	h := sha256.New()
	for _, f := range files {
		filename := filepath.Join(dir, f.Name())
		b, err := os.ReadFile(filename)
		if err != nil {
			return captureReference{}, fmt.Errorf("hash capture record %s: %w", filename, err)
		}
		_, _ = fmt.Fprintf(h, "%s %s\n", digest(b), f.Name())
	}
	return captureReference{Path: filepath.ToSlash(path), Hash: hex.EncodeToString(h.Sum(nil))}, nil
}

func (r *Runner) inspectCounts() error {
	dir := filepath.Join(r.IntentDir, "counts")
	files, err := readRetainedDirectory(dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		key, found := strings.CutSuffix(f.Name(), ".json")
		path := filepath.Join(dir, f.Name())
		if !found || !validKey(key) || !f.Type().IsRegular() {
			return fmt.Errorf("invalid classification count path %s", path)
		}
		if _, err = readCount(path, key); err != nil {
			return err
		}
	}
	return nil
}
