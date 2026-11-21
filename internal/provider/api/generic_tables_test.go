package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestListGenericTablesPaginates(t *testing.T) {
	var tokens []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-project-id"); got != "p1" {
			t.Errorf("x-project-id = %q, want p1", got)
		}
		if want := "/lakekeeper/v1/w1/namespaces/a\x1fb/generic-tables"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}

		token := r.URL.Query().Get("pageToken")
		tokens = append(tokens, token)

		w.Header().Set("Content-Type", "application/json")
		if token == "p2" {
			_, _ = io.WriteString(w, `{"identifiers":[{"namespace":["a","b"],"name":"two","id":"id-2","format":"delta"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"identifiers":[{"namespace":["a","b"],"name":"one","id":"id-1","format":"lance"}],"next-page-token":"p2"}`)
	}))
	defer srv.Close()

	client, err := NewStaticTokenClient(t.Context(), srv.URL, "token")
	if err != nil {
		t.Fatal(err)
	}

	got, err := client.ListGenericTables(t.Context(), "p1", "w1", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}

	want := []GenericTable{
		{ID: "id-1", Name: "one", Namespace: []string{"a", "b"}, Format: "lance"},
		{ID: "id-2", Name: "two", Namespace: []string{"a", "b"}, Format: "delta"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
	if !slices.Equal(tokens, []string{"", "p2"}) {
		t.Fatalf("pageToken sequence = %q, want [\"\" \"p2\"]", tokens)
	}
}

func TestCreateGenericTableSurvivesIDLookupFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"table":{"name":"gt","format":"lance","base-location":"s3://b/p"}}`)
			return
		}
		http.Error(w, "listing unavailable", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client, err := NewStaticTokenClient(t.Context(), srv.URL, "token")
	if err != nil {
		t.Fatal(err)
	}

	table, err := client.CreateGenericTable(t.Context(), "p1", "w1", []string{"a"}, GenericTableRequest{Name: "gt", Format: "lance"})
	if err != nil {
		t.Fatalf("CreateGenericTable returned an error after a successful create: %v", err)
	}
	if table.ID != "" {
		t.Fatalf("ID = %q, want empty after a failed lookup", table.ID)
	}
	if table.Name != "gt" {
		t.Fatalf("Name = %q, want gt", table.Name)
	}
}

func TestGenericTablesRejectPathSeparatorsInNames(t *testing.T) {
	client, err := NewStaticTokenClient(t.Context(), "http://127.0.0.1:1", "token")
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"sales/2024", "..", ".", "", "a\x1fb"} {
		if _, err := client.LoadGenericTable(t.Context(), "p1", "w1", []string{"a"}, name); err == nil || !strings.Contains(err.Error(), "invalid generic table name") {
			t.Errorf("LoadGenericTable(%q) error = %v, want invalid generic table name", name, err)
		}
		if err := client.DeleteGenericTable(t.Context(), "p1", "w1", []string{"a"}, name, true, false); err == nil || !strings.Contains(err.Error(), "invalid generic table name") {
			t.Errorf("DeleteGenericTable(%q) error = %v, want invalid generic table name", name, err)
		}
	}
}
