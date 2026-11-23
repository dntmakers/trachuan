package classify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"golang.org/x/sys/unix"
)

type openAIArchivePhase uint8

const (
	archiveNoDispatch openAIArchivePhase = iota
	archiveDispatchUncertain
	archiveVerifiedReceipt
)

type openAISubmitArchive struct {
	Phase          openAIArchivePhase
	Pair           openAIListingPair
	InputHash      string
	IDs            []string
	BatchID        string
	UploadStatus   int
	UploadBody     []byte
	ResponseStatus int
	ResponseBody   []byte
}

func archiveNamespace(sub int, s Submission) (bool, error) {
	entries, err := countEntries(sub)
	if err != nil {
		return false, err
	}
	finishedDispatched := false
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, s.ID) || name == s.ID+".json" || name == s.ID+".creator.lock" {
			continue
		}
		if name != s.ID+".creator.absent" && name != s.ID+".creator.finished" {
			return false, fmt.Errorf("prior submission evidence")
		}
		raw, err := submitRead(sub, name)
		var record creatorRecord
		if err != nil || validateRetainedJSON(raw, &record) != nil || json.Unmarshal(raw, &record) != nil || record.ID != s.ID || record.ItemsHash != s.ItemsHash || record.At.Before(s.CreatedAt) {
			return false, fmt.Errorf("invalid creator record")
		}
		if name == s.ID+".creator.finished" {
			finishedDispatched = record.Dispatched
		}
	}
	return finishedDispatched, nil
}

func readOpenAISubmitArchive(root string, s Submission, syncDir func(int) error) (openAISubmitArchive, error) {
	fail := func() (openAISubmitArchive, error) {
		return openAISubmitArchive{}, fmt.Errorf("invalid OpenAI submission archive")
	}
	if !validID(s.ID) || s.Provider != "openai" || s.Recipe.Provider != "openai" || !validID(s.AccountFingerprint) || s.State != "preparing" || s.BatchID != "" || s.SubmittedAt != nil || s.DeadlineAt != nil || s.Settled || s.CostMicroUSD != 0 || validateRecovery(s, s.ID) != nil {
		return fail()
	}
	st, err := openCountRoot(root)
	if err != nil {
		return fail()
	}
	defer st.close()
	for {
		err = unix.Flock(st.root, unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return fail()
	}
	sub, err := countOpenDir(st.root, "submissions", false)
	if err != nil {
		return fail()
	}
	defer func() { _ = unix.Close(sub) }()
	finishedDispatched, err := archiveNamespace(sub, s)
	if err != nil || submitOtherEvidence(st.root, s.ID) != nil || submitResultEvidence(st.root, s.ID) != nil {
		return fail()
	}
	raw, err := submitRead(sub, s.ID+".json")
	canonical, marshalErr := json.Marshal(s)
	if err != nil || marshalErr != nil || !bytes.Equal(raw, canonical) {
		return fail()
	}
	dirs := []int{}
	defer func() {
		for _, fd := range dirs {
			_ = unix.Close(fd)
		}
	}()
	dir := st.root
	for _, name := range []string{"evidence", s.ID, "openai-submit"} {
		fd, e := countOpenDir(dir, name, false)
		if errors.Is(e, unix.ENOENT) {
			dir = -1
			break
		}
		if e != nil {
			return fail()
		}
		dirs = append(dirs, fd)
		dir = fd
	}
	n := 0
	if dir >= 0 {
		entries, e := countEntries(dir)
		if e != nil {
			return fail()
		}
		for _, entry := range entries {
			if entry.Name() == ".staging" {
				stage, e := countOpenDir(dir, ".staging", false)
				if e != nil {
					return fail()
				}
				staged, e := countEntries(stage)
				_ = unix.Close(stage)
				if e != nil || len(staged) != 0 {
					return fail()
				}
			} else {
				n++
			}
		}
		if n > len(submitNames) {
			return fail()
		}
	}
	disk := &openAISubmitDisk{}
	records, err := disk.history(dir, s, n)
	if err != nil || (finishedDispatched && n < 4) {
		return fail()
	}
	for i, r := range records {
		raw, readErr := submitRead(dir, submitNames[i])
		canonical, marshalErr := json.Marshal(r)
		if readErr != nil || marshalErr != nil || !bytes.Equal(raw, canonical) {
			return fail()
		}
		if i > 0 && r.At.Before(records[i-1].At) {
			return fail()
		}
		expected := disk.scope(s)
		expected.At = r.At
		switch i {
		case 0:
			expected.InputHash, expected.IDs = r.InputHash, r.IDs
		case 1, 4:
			expected.Status, expected.Body = r.Status, r.Body
			if len(r.Body) > 4<<20 {
				return fail()
			}
		case 2:
			expected.InputHash, expected.ResponseHash, expected.FileID = r.InputHash, r.ResponseHash, r.FileID
		case 3:
			expected.InputHash, expected.FileID, expected.SubmissionID = r.InputHash, r.FileID, r.SubmissionID
		case 5:
			expected.InputHash, expected.ResponseHash, expected.FileID, expected.SubmissionID, expected.BatchID = r.InputHash, r.ResponseHash, r.FileID, r.SubmissionID, r.BatchID
		}
		if !reflect.DeepEqual(r, expected) {
			return fail()
		}
	}
	result := openAISubmitArchive{}
	if n > 0 {
		result.InputHash, result.IDs = records[0].InputHash, records[0].IDs
	}
	if n >= 2 {
		result.UploadStatus, result.UploadBody = records[1].Status, records[1].Body
	}
	if n >= 4 {
		result.Phase = archiveDispatchUncertain
		result.Pair = openAIListingPair{SubmissionID: s.ID, InputFileID: records[3].FileID}
	}
	if n >= 5 {
		result.ResponseStatus, result.ResponseBody = records[4].Status, records[4].Body
	}
	if n == 6 {
		response, receipt := records[4], records[5]
		id, e := openAICreateID(response.Body, result.Pair.InputFileID, s.ID)
		if e != nil || response.Status < 200 || response.Status >= 300 || response.Status == 206 || receipt.BatchID != id || receipt.FileID != result.Pair.InputFileID || receipt.SubmissionID != s.ID || receipt.InputHash != result.InputHash || receipt.ResponseHash != digest(response.Body) {
			return fail()
		}
		result.Phase, result.BatchID = archiveVerifiedReceipt, id
	}
	if syncDir == nil {
		syncDir = unix.Fsync
	}
	for _, fd := range append([]int{st.parent, st.root, sub}, dirs...) {
		if syncDir(fd) != nil {
			return fail()
		}
	}
	return result, nil
}
