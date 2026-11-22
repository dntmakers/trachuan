package taxonomy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFrozenSource(t *testing.T) {
	root := "../evaluation/testdata/vn"
	path := filepath.Join(root, "taxonomy-post-2023.json")
	source, err := ReadFrozenSource(path, "post-2023", "vn", "20261115_3")
	if err != nil || len(source.Articles) == 0 {
		t.Fatalf("source: %v", err)
	}
	if _, err = ReadFrozenSource(path, "multi-family", "vn", "20261115_3"); err == nil {
		t.Fatal("accepted wrong slice")
	}
	if _, err = ReadFrozenSource(path, "post-2023", "vn", "20990101"); err == nil {
		t.Fatal("accepted wrong release")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	altered := filepath.Join(t.TempDir(), "altered.json")
	if err = os.WriteFile(altered, append(data, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadFrozenSource(altered, "post-2023", "vn", "20261115_3"); err == nil {
		t.Fatal("accepted altered file")
	}
}

func TestPacketFromFrozen(t *testing.T) {
	source := FrozenLabels{Country: "vn", Release: "r", DrawRule: "rule", Articles: []LabelArticle{{Key: "b", Anchor: "2", OwnTextSHA256: OwnTextSHA256("second"), Codes: []string{"secret"}}, {Key: "a", Anchor: "1", OwnTextSHA256: OwnTextSHA256("first")}}}
	rows := []DrawArticle{{PacketArticle: PacketArticle{Key: "a", Anchor: "1", Text: "first", Number: "n1", Title: "t1"}}, {PacketArticle: PacketArticle{Key: "b", Anchor: "2", Text: "second", Number: "n2", Title: "t2"}}}
	tree := []PacketNode{{Code: "1", Name: "node"}}
	packet, err := PacketFromFrozen(source, rows, tree, "post-2023")
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Articles) != 2 || packet.Articles[0].Key != "b" || packet.Articles[0].Text != "second" || packet.Articles[0].Number != "n2" || packet.Articles[0].Title != "t2" || packet.LabelingRule != LabelingRule || len(packet.Tree) != 1 {
		t.Fatalf("bad packet: %+v", packet)
	}
	rows[1].Text = "changed"
	if _, err = PacketFromFrozen(source, rows, tree, "post-2023"); err == nil {
		t.Fatal("accepted changed text")
	}
}

func TestSourceListSHA256(t *testing.T) {
	a := []LabelArticle{{Key: "b", Anchor: "2", OwnTextSHA256: "b"}, {Key: "a", Anchor: "1", OwnTextSHA256: "a"}}
	b := []LabelArticle{a[1], a[0]}
	h1, err := SourceListSHA256(a)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := SourceListSHA256(b)
	if err != nil || h1 != h2 {
		t.Fatalf("order: %s %s %v", h1, h2, err)
	}
	b[0].OwnTextSHA256 = "changed"
	h2, err = SourceListSHA256(b)
	if err != nil || h1 == h2 {
		t.Fatal("ignored hash")
	}
	if _, err = SourceListSHA256([]LabelArticle{a[0], a[0]}); err == nil {
		t.Fatal("accepted duplicate identity")
	}
	escaped, err := SourceListSHA256([]LabelArticle{{Key: "d\"\\\n", Anchor: "đ", OwnTextSHA256: "hash"}})
	if err != nil || escaped != "fbdaddc7d04f8107e4582050fd07d1fddae091be784114a5f103b2ecf9789779" {
		t.Fatalf("escaped hash %s: %v", escaped, err)
	}
}

func TestProviderRestrictions(t *testing.T) {
	p := Packet{Country: "vn", Release: "r", Slice: "post-2023", DrawRule: "rule", Articles: []PacketArticle{{Key: "doc", Anchor: "1", Text: "text", OwnTextSHA256: OwnTextSHA256("text")}}, Tree: []PacketNode{{Code: "1.1", Level: 2}}}
	labels := []LabelArticle{{Key: "doc", Anchor: "1", Codes: []string{"1.1"}}}
	for _, labeler := range []string{"gpt-6-astra", "claude-invalid"} {
		if _, err := FreezeProviderPacket(p, labels, "anthropic", labeler); err == nil {
			t.Errorf("accepted %s", labeler)
		}
	}
	for _, labeler := range []string{"claude-fable-5-2", "claude-opus-5-5"} {
		if _, err := FreezeProviderPacket(p, labels, "anthropic", labeler); err != nil {
			t.Errorf("%s: %v", labeler, err)
		}
	}
	p.LabelerFragments = 5
	if _, err := FreezeProviderPacket(p, labels, "anthropic", "claude-opus-5-5"); err == nil {
		t.Error("accepted source fragments")
	}
	p.LabelerFragments = 0
	for _, bad := range [][]LabelArticle{nil, {{Key: "doc", Anchor: "1", Codes: []string{}}}} {
		if _, err := FreezeProviderPacket(p, bad, "anthropic", "claude-opus-5-5"); err == nil {
			t.Error("accepted empty labels")
		}
	}
}

func TestFrozenPacketClearsFragments(t *testing.T) {
	source := FrozenLabels{LabelerFragments: 5, Articles: []LabelArticle{{Key: "d", Anchor: "a", OwnTextSHA256: OwnTextSHA256("text")}}}
	packet, err := PacketFromFrozen(source, []DrawArticle{{PacketArticle: PacketArticle{Key: "d", Anchor: "a", Text: "text"}}}, nil, "post-2023")
	if err != nil || packet.LabelerFragments != 0 {
		t.Fatalf("packet=%+v err=%v", packet, err)
	}
}
