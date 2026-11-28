package lawkey

import "testing"

func TestPlainAnchor(t *testing.T) {
	for _, tc := range []struct {
		anchor string
		valid  bool
	}{{"dieu-1/khoan-2/diem-a", true}, {"phan-1/chuong-2/muc-3/other-x", true}, {"dieu-a#b", true}, {"dieu-a?b", true}, {"dieu-a\x00b", false}, {"dieu-a\x7fb", false}, {"invented-a", true}, {"dieu-1~2", true}, {"dieu-", false}} {
		if PlainAnchor(tc.anchor) != tc.valid {
			t.Errorf("%q: wanted %t", tc.anchor, tc.valid)
		}
	}
}

func TestPlainAnchorAcceptsParserShapes(t *testing.T) {
	for _, anchor := range []string{"doan-1", "kem-theo-1/dieu-1", "dieu-1#2", "dieu-1?2"} {
		if !PlainAnchor(anchor) {
			t.Errorf("rejected parser anchor %q", anchor)
		}
	}
}
