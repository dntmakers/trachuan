package classify

import (
	"fmt"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
)

func TestUnseededSelectionPartition(t *testing.T) {
	var input []taxonomy.LabelArticle
	for i := 0; i < 100; i++ {
		input = append(input, taxonomy.LabelArticle{Key: fmt.Sprintf("doc-%d", i), Anchor: "dieu-1", OwnTextSHA256: "x"}, taxonomy.LabelArticle{Key: fmt.Sprintf("doc-%d", i), Anchor: "dieu-2", OwnTextSHA256: "y"})
	}
	seen := map[string]int{}
	for part := 1; part <= 7; part++ {
		a, hash, err := UnseededSelection("vn", "r", input, part, 7)
		if err != nil {
			t.Fatal(err)
		}
		b, again, err := UnseededSelection("vn", "r", input, part, 7)
		if err != nil || hash != again || len(a.Articles) != len(b.Articles) {
			t.Fatal("nondeterministic selection")
		}
		for _, article := range a.Articles {
			if old := seen[article.Key]; old != 0 && old != part {
				t.Fatal("document split")
			}
			seen[article.Key] = part
		}
	}
	if len(seen) != 100 {
		t.Fatalf("got %d documents", len(seen))
	}
	_, a, _ := UnseededSelection("vn", "r", nil, 1, 7)
	_, b, _ := UnseededSelection("vn", "r", nil, 2, 7)
	if a == b {
		t.Fatal("part missing from identity")
	}
}

func TestOpenAITrialStateModeBounds(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err != nil {
		t.Fatal(err)
	}
	state := loadTrialFixtureState(t, o.Out)
	state.ReserveMicroUSD = 40_000_001
	if state.valid() {
		t.Fatal("legacy labels state accepted above 40 USD")
	}
	state.SelectionMode = "labels"
	if state.valid() {
		t.Fatal("labels state accepted above 40 USD")
	}
	state.SelectionMode = "unseeded"
	state.Selection = "unseeded:1/8"
	if !state.valid() {
		t.Fatal("unseeded state rejected within 175 USD")
	}
	state.ReserveMicroUSD = 175_000_001
	if state.valid() {
		t.Fatal("unseeded state accepted above 175 USD")
	}
	state.ReserveMicroUSD = 40_000_000
	state.SiblingMicroUSD = 40_000_001
	state.SelectionMode = "labels"
	state.Selection = ""
	if state.valid() {
		t.Fatal("labels sibling bound exceeded")
	}
	state.SelectionMode = "unknown"
	if state.valid() {
		t.Fatal("unknown selection mode accepted")
	}
}

func TestUnseededPaidResumeWithoutSelectionHash(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	o.Unseeded = true
	o.Selection = "unseeded:1/8"
	wire.pause = "GET /v1/batches/batch-one"
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
		t.Fatal("expected interrupted collection")
	}
	wire.pause = ""
	wire.calls = nil
	resumed := o
	resumed.LabelsHash = ""
	wrong := resumed
	wrong.Selection = "unseeded:2/8"
	if _, _, err := ResumeOpenAITrial(t.Context(), "documents", wrong, wire.client()); err == nil || len(wire.calls) != 0 {
		t.Fatal("accepted different part")
	}
	if _, handled, err := ResumeOpenAITrial(t.Context(), "documents", resumed, wire.client()); err != nil || !handled {
		t.Fatalf("paid resume failed: handled=%t err=%v", handled, err)
	}
}

func TestUnseededBuildIdentity(t *testing.T) {
	selection := strings.Repeat("a", 64)
	input := strings.Repeat("b", 64)
	recipe := strings.Repeat("c", 64)
	original, err := UnseededBuildHash(selection, input, recipe)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{strings.Repeat("d", 64), recipe}, {input, strings.Repeat("d", 64)}} {
		changed, err := UnseededBuildHash(selection, pair[0], pair[1])
		if err != nil || original == changed {
			t.Fatal("build change did not change identity", err)
		}
	}
	if _, err := UnseededBuildHash(selection, "", recipe); err == nil {
		t.Fatal("accepted invalid build hash")
	}
}

func TestUnseededUndispatchedRequiresCurrentSelection(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	o.Unseeded = true
	o.Selection = "unseeded:1/8"
	stopTrialAfterUpload(t, o, docs, menu, wire)
	noHash := o
	noHash.LabelsHash = ""
	wire.calls = nil
	if _, handled, err := ResumeOpenAITrial(t.Context(), "documents", noHash, wire.client()); handled || err != nil || len(wire.calls) != 0 {
		t.Fatalf("undispatched resume skipped release selection: handled=%v err=%v", handled, err)
	}
	changed := o
	changed.LabelsHash = strings.Repeat("c", 64)
	if _, handled, err := ResumeOpenAITrial(t.Context(), "documents", changed, wire.client()); !handled || err == nil || len(wire.calls) != 0 {
		t.Fatalf("accepted changed selection: handled=%v err=%v", handled, err)
	}
}
