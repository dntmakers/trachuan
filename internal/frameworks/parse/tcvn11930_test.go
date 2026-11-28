package parse

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const tcvnTestSHA = "9f46c1956803eb0c72c3709832c8442ea7df837c99ad5b8ce68804e583e93f71"

func tcvnFixture() []string {
	pages := make([]string, 67)
	pages[14] = "TCVN 11930:2017\n" + strings.Repeat("\n", 23) + "5 Alpha\n\n5.1 Beta\n\n8 Gamma\n\n8.1 Delta\n\n8.1.3 Epsilon\n\n8.1.3.1 Zeta\n\n8.1.3.2 Eta\n\n8.1.3.2 Theta\n\n8.1.4 Iota\n\n9 Kappa\n\n9.1 Lambda\n\n9.1.3 Mu\n\n9.1.3.1 Nu\n\n9.1.3.2 Xi\n\n9.1.3.2 Omicron\n\n9.1.4 Pi\n\n15\n"
	pages[66] = "TCVN 11930:2017\n\n" + strings.Repeat(" ", 46) + "Annex A\n\nIntroduction\ncontinued\ncontinued\nA.1 Rho\n\n67\n"
	for _, heading := range []string{"5.1 Beta", "8.1.3.1 Zeta", "8.1.3.2 Eta", "8.1.3.2 Theta", "8.1.4 Iota", "9.1.3.1 Nu", "9.1.3.2 Xi", "9.1.3.2 Omicron", "9.1.4 Pi"} {
		pages[14] = strings.Replace(pages[14], heading+"\n\n", heading+"\n\nFixture body.\n\n", 1)
	}
	pages[66] = strings.Replace(pages[66], "A.1 Rho\n\n", "A.1 Rho\n\nFixture body.\n\n", 1)
	return pages
}

func tcvnSyntheticPages(pages []string, expected *int) (*Result, error) {
	nodes, err := parseTCVN11930(pages)
	return finish(nodes, err, expected)
}

func TestTCVN11930_Inventory(t *testing.T) {
	pages := tcvnFixture()
	count := 15
	got, err := tcvnSyntheticPages(pages, &count)
	if err != nil {
		t.Fatal(err)
	}
	citations := []string{"5", "5.1", "8", "8.1", "8.1.3", "8.1.3.1", "8.1.3.2", "8.1.4", "9", "9.1", "9.1.3", "9.1.3.1", "9.1.3.2", "9.1.4", "A.1"}
	parents := []int{-1, 0, -1, 2, 3, 4, 4, 3, -1, 8, 9, 10, 10, 9, -1}
	kinds := []string{"clause", "control", "clause", "clause", "clause", "control", "control", "control", "clause", "clause", "clause", "control", "control", "control", "control"}
	var actual []string
	for i, node := range got.Nodes {
		actual = append(actual, node.Citation)
		if node.Parent != parents[i] || node.Kind != kinds[i] || node.Ordinal != i {
			t.Fatalf("node %d has wrong parent, kind, or ordinal", i)
		}
	}
	if !reflect.DeepEqual(actual, citations) {
		t.Fatalf("wrong citations: %v", actual)
	}
}

func TestTCVN11930_Bodies(t *testing.T) {
	pages := tcvnFixture()
	for _, heading := range []string{"5.1 Beta", "8.1.3.2 Eta", "8.1.3.2 Theta", "9.1.3.2 Xi", "9.1.3.2 Omicron"} {
		pages[14] = strings.Replace(pages[14], heading+"\n\nFixture body.\n\n", heading+"\n\n", 1)
	}
	pages[66] = strings.Replace(pages[66], "A.1 Rho\n\nFixture body.\n\n", "A.1 Rho\n\n", 1)
	pages[66] = strings.Replace(pages[66], "A.1 Rho\n\n", "A.1 Rho\n\nĐoạn cuối.\n\n", 1)
	pages[14] = strings.Replace(pages[14], "5.1 Beta\n\n", "5.1 Beta\n\nĐường dẫn\ntiếp theo.\n\nĐoạn mới.\n- Mục một\n  nối tiếp\nđ) Mục hai\n\n", 1)
	pages[14] = strings.Replace(pages[14], "8.1.3.2 Eta\n\n", "8.1.3.2 Eta\n\nPhần trước\n", 1)
	pages[14] = strings.Replace(pages[14], "8.1.3.2 Theta\n\n", "8.1.3.2 Theta\n\nPhần sau.\n\n", 1)
	pages[14] = strings.Replace(pages[14], "9.1.3.2 Xi\n\n", "9.1.3.2 Xi\n\nTrước.\n\n", 1)
	pages[14] = strings.Replace(pages[14], "9.1.3.2 Omicron\n\n", "9.1.3.2 Omicron\n\nSau.\n\n", 1)
	pages[14] = strings.Replace(pages[14], "Phần trước\n", "Phần trước\n\n", 1)
	before, after, _ := strings.Cut(pages[14], "\n8.1.3.2 Theta")
	pages[14] = before + "\n\n15\n"
	pages[15] = "TCVN 11930:2017\n\n8.1.3.2 Theta" + after
	pages[15] = strings.TrimSuffix(pages[15], "15\n") + "16\n"
	count := 15
	result, err := tcvnSyntheticPages(pages, &count)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"5.1":     "Đường dẫn tiếp theo.\n\nĐoạn mới.\n- Mục một nối tiếp\nđ) Mục hai",
		"8.1.3.2": "Phần trước\n\n8.1.3.2 Theta\n\nPhần sau.",
		"9.1.3.2": "Trước.\n\n9.1.3.2 Omicron\n\nSau.",
		"A.1":     "Đoạn cuối.",
	}
	for _, node := range result.Nodes {
		if expected, ok := want[node.Citation]; ok && node.Body != expected {
			t.Errorf("body for %s = %q, want %q", node.Citation, node.Body, expected)
		}
	}
}

func TestTCVN11930_PageBreakWrap(t *testing.T) {
	pages := tcvnFixture()
	pages[14] = strings.Replace(pages[14], "5.1 Beta\n\nFixture body.\n\n", "5.1 Beta\n\nMột dòng\n", 1)
	before, after, _ := strings.Cut(pages[14], "\n8 Gamma")
	pages[14] = before + "\n\n15\n"
	pages[15] = "TCVN 11930:2017\n\n  tiếp tục.\n\n8 Gamma" + after
	pages[15] = strings.TrimSuffix(pages[15], "15\n") + "16\n"
	count := 15
	result, err := tcvnSyntheticPages(pages, &count)
	if err != nil {
		t.Fatal(err)
	}
	if result.Nodes[1].Body != "Một dòng tiếp tục." {
		t.Fatalf("unexpected wrapped body %q", result.Nodes[1].Body)
	}
}

func TestTCVN11930_ReviewCases(t *testing.T) {
	t.Run("anchor uses position", func(t *testing.T) {
		pages := tcvnFixture()
		pages[66] = strings.Replace(pages[66], "Annex A", "Annex B", 1)
		count := 15
		if _, err := tcvnSyntheticPages(pages, &count); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("misplaced duplicate keeps citation", func(t *testing.T) {
		pages := tcvnFixture()
		pages[14] = strings.Replace(pages[14], "8.1.3.2 Theta", "8.1.3.3 Inserted\n\nInserted body.\n\n8.1.3.2 Theta", 1)
		count := 16
		_, err := tcvnSyntheticPages(pages, &count)
		if err == nil || err.Error() != "invalid or duplicate citations" {
			t.Fatalf("unexpected error %v", err)
		}
	})
	t.Run("count mismatch", func(t *testing.T) {
		count := 16
		_, err := tcvnSyntheticPages(tcvnFixture(), &count)
		if err == nil || err.Error() != "control count mismatch" {
			t.Fatalf("unexpected error %v", err)
		}
	})
	t.Run("deterministic", func(t *testing.T) {
		count := 15
		first, err := tcvnSyntheticPages(tcvnFixture(), &count)
		if err != nil {
			t.Fatal(err)
		}
		second, err := tcvnSyntheticPages(tcvnFixture(), &count)
		if err != nil || !reflect.DeepEqual(first, second) {
			t.Fatalf("nondeterministic result: %v", err)
		}
	})
}

func TestTCVN11930_LineMemory(t *testing.T) {
	pages := tcvnFixture()
	pages[15] = strings.Repeat("\n", 1<<20)
	count := 15
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := tcvnSyntheticPages(pages, &count)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if used := after.TotalAlloc - before.TotalAlloc; used > 4<<20 {
		t.Fatalf("line traversal allocated %d bytes", used)
	}
}

func TestTCVN11930_Errors(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func([]string)
		sha, want string
	}{
		{"fingerprint", func([]string) {}, "wrong", "source fingerprint mismatch"},
		{"missing region", func(p []string) { p[66] = "" }, tcvnTestSHA, "missing required region"},
		{"missing duplicate", func(p []string) { p[14] = strings.Replace(p[14], "8.1.3.2 Theta\n\n", "", 1) }, tcvnTestSHA, "invalid or duplicate citations"},
		{"extra duplicate", func(p []string) { p[14] = strings.Replace(p[14], "8.1.3.2 Theta", "8.1.3.2 Theta\n\n8.1.3.2 Extra", 1) }, tcvnTestSHA, "invalid or duplicate citations"},
		{"invalid footer", func(p []string) { p[14] = strings.TrimSuffix(p[14], "15\n") + "PRIVATE_SENTINEL\n" }, tcvnTestSHA, "invalid page furniture"},
		{"misplaced duplicate", func(p []string) { p[14] = strings.Replace(p[14], "8.1.3.2 Theta", "8.1.4 Theta", 1) }, tcvnTestSHA, "invalid or duplicate citations"},
		{"stale parent", func(p []string) { p[14] = strings.Replace(p[14], "9 Kappa", "5.2 Stale\n\n9 Kappa", 1) }, tcvnTestSHA, "missing heading parent"},
		{"wrong annex position", func(p []string) {
			p[66] = strings.Replace(p[66], strings.Repeat(" ", 46)+"Annex A", strings.Repeat(" ", 45)+"Annex A", 1)
		}, tcvnTestSHA, "missing required region"},
		{"empty leaf", func(p []string) { p[66] = strings.Replace(p[66], "A.1 Rho\n\nFixture body.\n\n", "A.1 Rho\n\n", 1) }, tcvnTestSHA, "missing control body"},
		{"empty first duplicate passage", func(p []string) {
			p[14] = strings.Replace(p[14], "8.1.3.2 Eta\n\nFixture body.\n\n", "8.1.3.2 Eta\n\n", 1)
		}, tcvnTestSHA, "missing duplicate passage"},
		{"empty second duplicate passage", func(p []string) {
			p[14] = strings.Replace(p[14], "8.1.3.2 Theta\n\nFixture body.\n\n", "8.1.3.2 Theta\n\n", 1)
		}, tcvnTestSHA, "missing duplicate passage"},
		{"empty first ninth passage", func(p []string) {
			p[14] = strings.Replace(p[14], "9.1.3.2 Xi\n\nFixture body.\n\n", "9.1.3.2 Xi\n\n", 1)
		}, tcvnTestSHA, "missing duplicate passage"},
		{"empty second ninth passage", func(p []string) {
			p[14] = strings.Replace(p[14], "9.1.3.2 Omicron\n\nFixture body.\n\n", "9.1.3.2 Omicron\n\n", 1)
		}, tcvnTestSHA, "missing duplicate passage"},
		{"missing parent", func(p []string) { p[14] = strings.Replace(p[14], "8.1.3 Epsilon", "8.2.3 Epsilon", 1) }, tcvnTestSHA, "missing heading parent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pages := tcvnFixture()
			tc.mutate(pages)
			count := 15
			var err error
			if tc.sha == "wrong" {
				_, err = ParsePages("tcvn11930-2017-pdf", pages, "tcvn11930", "2017", tc.sha, &count)
			} else {
				_, err = tcvnSyntheticPages(pages, &count)
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error %v, want %s", err, tc.want)
			}
		})
	}
}
