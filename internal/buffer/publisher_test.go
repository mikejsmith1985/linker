package buffer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikejsmith1985/linker/internal/store"
)

func TestComposeText(t *testing.T) {
	got := ComposeText(store.Post{Content: "Body.", Hashtags: "#A #B"})
	if got != "Body.\n\n#A #B" {
		t.Errorf("got %q", got)
	}
	if got := ComposeText(store.Post{Content: "Body only", Hashtags: "  "}); got != "Body only" {
		t.Errorf("got %q, want body without trailing tags", got)
	}
}

func TestLiveClientQueueSuccess(t *testing.T) {
	var gotForm string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/updates/create.json") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		gotForm = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"updates":[{"id":"buf_123"}]}`))
	}))
	defer srv.Close()

	c := NewLiveClient("tok", "prof_1")
	c.baseURL = srv.URL

	id, err := c.Queue(context.Background(), store.Post{Content: "Hello", Hashtags: "#Go"})
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if id != "buf_123" {
		t.Errorf("id = %q, want buf_123", id)
	}
	for _, want := range []string{"access_token=tok", "profile_ids", "prof_1", "text="} {
		if !strings.Contains(gotForm, want) {
			t.Errorf("form missing %q; got %q", want, gotForm)
		}
	}
}

func TestLiveClientQueueErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"message":"bad token"}`))
	}))
	defer srv.Close()

	c := NewLiveClient("tok", "prof_1")
	c.baseURL = srv.URL
	if _, err := c.Queue(context.Background(), store.Post{Content: "x"}); err == nil {
		t.Fatal("expected error on non-2xx status")
	}
}

func TestLiveClientQueueNoUpdateID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"updates":[]}`))
	}))
	defer srv.Close()

	c := NewLiveClient("tok", "prof_1")
	c.baseURL = srv.URL
	if _, err := c.Queue(context.Background(), store.Post{Content: "x"}); err == nil {
		t.Fatal("expected error when no update id returned")
	}
}

func TestStubQueueUniqueIDs(t *testing.T) {
	s := NewStub(nil)
	id1, err := s.Queue(context.Background(), store.Post{ID: 1, Content: "a"})
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	id2, _ := s.Queue(context.Background(), store.Post{ID: 2, Content: "b"})
	if id1 == id2 {
		t.Errorf("expected unique ids, got %q twice", id1)
	}
	if !strings.HasPrefix(id1, "stub-") {
		t.Errorf("id = %q, want stub- prefix", id1)
	}
}
