package retrieval

import "testing"

func TestRecipe4Supported(t *testing.T) {
	r := Recipe3()
	r.Version = 4
	r.QueryGate = "vn-restore/1"
	if !r.Supported() {
		t.Fatal("recipe 4 is not supported")
	}
}

func TestRecipeRouting(t *testing.T) {
	for _, recipe := range []Recipe{Recipe2(), Recipe3(), Recipe4()} {
		for _, tc := range []struct {
			query  string
			route  queryRoute
			reason string
		}{
			{"50/2024/TT-NHNN", routeNumber, ""},
			{"ngân hàng", routeMarked, ""},
			{"ngan hang", routeRestore, "diacritics_required"},
			{"123 %", routeUnsupported, "unsupported_query"},
		} {
			v, err := ValidateQuery(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			v = resolveValidation(v, recipe, false)
			reason := tc.reason
			if recipe.Version == 4 && tc.route == routeRestore {
				reason = ""
			}
			if v.Route != tc.route || v.Reason != reason || v.Accepted != (reason == "") {
				t.Fatalf("recipe %d query %q: %+v", recipe.Version, tc.query, v)
			}
			if tc.route == routeUnsupported && recipe.Version == 4 && v.Message != "Vui lòng nhập câu hỏi tiếng Việt hoặc số hiệu văn bản." {
				t.Fatal(v.Message)
			}
		}
	}
}
