package provider

import "testing"

func TestGenericTableHelpers(t *testing.T) {
	t.Run("split nested namespace", func(t *testing.T) {
		got, err := splitNamespacePath("sales.bronze")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0] != "sales" || got[1] != "bronze" {
			t.Fatalf("splitNamespacePath = %q, want [sales bronze]", got)
		}
	})

	for _, input := range []string{"", "sales..bronze", "sales.bronze.", ".sales", "sales/bronze", "sales\x1fbronze"} {
		t.Run("rejects "+input, func(t *testing.T) {
			if _, err := splitNamespacePath(input); err == nil {
				t.Fatalf("splitNamespacePath(%q) = nil error, want error", input)
			}
		})
	}

	t.Run("join namespace", func(t *testing.T) {
		if got := joinNamespacePath([]string{"sales", "bronze"}); got != "sales.bronze" {
			t.Fatalf("joinNamespacePath = %q, want sales.bronze", got)
		}
	})

	t.Run("internal ID", func(t *testing.T) {
		got := genericTableInternalID("p", "wh", []string{"sales", "bronze"}, "t")
		if want := "p/wh/sales.bronze/t"; got != want {
			t.Fatalf("genericTableInternalID = %q, want %q", got, want)
		}
	})
}

func TestCanonicalJSON(t *testing.T) {
	in := []byte(`{"z":9007199254740993,"a":{"c":2,"b":1}}`)
	want := `{"a":{"b":1,"c":2},"z":9007199254740993}`
	if got := canonicalJSON(in); got != want {
		t.Fatalf("canonicalJSON = %s, want %s", got, want)
	}
}

func TestCanonicalJSONKeepsHTMLCharacters(t *testing.T) {
	in := []byte(`{"type":"list<float>","doc":"a & b"}`)
	want := `{"doc":"a & b","type":"list<float>"}`
	if got := canonicalJSON(in); got != want {
		t.Fatalf("canonicalJSON = %s, want %s", got, want)
	}
}
