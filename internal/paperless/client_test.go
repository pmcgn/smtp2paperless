package paperless

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUploadPath(t *testing.T) {
	tests := []struct {
		name, path, want string
	}{
		{"default", "", "/api/documents/post_document/"},
		{"custom with slash", "/api/custom/upload/", "/api/custom/upload/"},
		{"custom without slash", "custom/upload", "/custom/upload"},
		{"whitespace only", "   ", "/api/documents/post_document/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Path
				w.Write([]byte(`"task-id"`))
			}))
			defer srv.Close()

			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			c := New(srv.URL+"/", "tok", 5*time.Second, 0, Options{Path: tt.path}, nil, log)
			if _, err := c.Upload(context.Background(), "a.pdf", "application/pdf", []byte("x")); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("path = %q, want %q", got, tt.want)
			}
		})
	}
}
