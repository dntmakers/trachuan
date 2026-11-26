package evidence

import (
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage/build"
	"danny.vn/trachuan/apps/corpus/internal/stage/membership"
)

func memberInventory() build.Inventory {
	replacement := "vn:vbpl:2099004"
	entries := []membership.Entry{
		{DocumentKey: "vn:vbpl:2099001", Disposition: "active"},
		{DocumentKey: "vn:vbpl:2099002", Disposition: "withdrawn"},
		{DocumentKey: "vn:vbpl:2099005", Disposition: "scope_removed"},
		{DocumentKey: "vn:chinhphu:2099003", Disposition: "source_replaced", ReplacementKey: &replacement},
		{DocumentKey: "vn:chinhphu:2099008", Disposition: "source_replaced"},
	}
	return build.Inventory{Version: 2, Country: "vn", FinalMembershipHash: strings.Repeat("f", 64), Membership: membership.PhaseSnapshot{Entries: entries}}
}

func TestMembersFrom_ReadsDispositionsAndHandOvers(t *testing.T) {
	inv := memberInventory()
	m, err := membersFrom(inv, inv.IdentityHash())
	if err != nil {
		t.Fatal(err)
	}
	if !m.IsActive("vn:vbpl:2099001") || m.IsActive("vn:vbpl:2099002") {
		t.Fatal("incorrect active members")
	}
	if m.Disposition("vn:vbpl:2099002") != "withdrawn" || m.Disposition("vn:chinhphu:2099008") != "source_replaced" || len(m.Entries()) != 5 {
		t.Fatal("incorrect dispositions")
	}
	if replacement, ok := m.Replacement("vn:chinhphu:2099003"); !ok || replacement != "vn:vbpl:2099004" {
		t.Fatalf("replacement %q, found %v", replacement, ok)
	}
	if _, ok := m.Replacement("vn:chinhphu:2099008"); ok {
		t.Fatal("unexpected replacement")
	}
}

func TestMembersFrom_ReturnsIndependentEntries(t *testing.T) {
	inv := memberInventory()
	m, err := membersFrom(inv, inv.IdentityHash())
	if err != nil {
		t.Fatal(err)
	}
	entries := m.Entries()
	for i := range entries {
		entries[i].Disposition = "withdrawn"
		if entries[i].ReplacementKey != nil {
			*entries[i].ReplacementKey = "changed"
		}
	}
	if !m.IsActive("vn:vbpl:2099001") {
		t.Fatal("changed returned entry altered active members")
	}
	if replacement, _ := m.Replacement("vn:chinhphu:2099003"); replacement != "vn:vbpl:2099004" {
		t.Fatalf("changed returned entry altered replacement: %s", replacement)
	}
}

func TestMembersFrom_RefusesUnboundOrLegacyInventory(t *testing.T) {
	inv := memberInventory()
	if _, err := membersFrom(inv, strings.Repeat("0", 64)); err == nil || !strings.Contains(err.Error(), "differs from the build") {
		t.Fatalf("other build: %v", err)
	}
	legacy := inv
	legacy.Version = 0
	if _, err := membersFrom(legacy, legacy.IdentityHash()); err == nil || !strings.Contains(err.Error(), "membership") {
		t.Fatalf("legacy: %v", err)
	}
	changed := memberInventory()
	changed.FinalMembershipHash = strings.Repeat("e", 64)
	if _, err := membersFrom(changed, inv.IdentityHash()); err == nil {
		t.Fatal("membership changed after build accepted")
	}
}
