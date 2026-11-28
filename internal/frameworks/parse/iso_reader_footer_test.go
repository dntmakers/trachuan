package parse

import "testing"

func TestISOReader_LastFooterOnly(t *testing.T) {
	r := isoReader{pages: []string{"Before\n42\nFOOTER\n", "FOOTER\nAfter\n"}, footerLast: func(line isoLine) bool { return line.text == "FOOTER" }}
	want := []string{"Before", "42", "FOOTER", "After"}
	for i, expected := range want {
		line, ok := r.next()
		if !ok || line.text != expected {
			t.Fatalf("line %d: got %q", i, line.text)
		}
	}
	if _, ok := r.next(); ok {
		t.Fatal("unexpected line")
	}
}
