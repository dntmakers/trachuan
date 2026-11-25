package webapi

import "testing"

func TestSearchUnfilteredResponseGolden(t *testing.T) {
	h := newHarness(t)
	w := webCall(h, "POST", "/api/v1/search", `{"country":"vn","query":"ngân hàng"}`)
	const want = `{"abstain":false,"hits":[{"complete":true,"doc":"vn:1","handle":"vn:1#a","snippet":"verbatim"}],"release":"20991110_1"}`
	if w.Code != 200 || w.Body.String() != want {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
}
