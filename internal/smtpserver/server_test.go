package smtpserver_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/pmcgn/smtp2paperless/internal/mailparse"
	"github.com/pmcgn/smtp2paperless/internal/metrics"
	"github.com/pmcgn/smtp2paperless/internal/paperless"
	"github.com/pmcgn/smtp2paperless/internal/smtpserver"
)

const mail = "From: s@example.local\r\nTo: p@example.local\r\nSubject: scan\r\nMIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=b\r\n\r\n" +
	"--b\r\nContent-Type: text/plain\r\n\r\nbody\r\n" +
	"--b\r\nContent-Type: application/pdf\r\nContent-Transfer-Encoding: base64\r\n" +
	"Content-Disposition: attachment; filename=\"a.pdf\"\r\n\r\nSGVsbG8=\r\n" +
	"--b\r\nContent-Type: application/zip\r\nContent-Transfer-Encoding: base64\r\n" +
	"Content-Disposition: attachment; filename=\"a.zip\"\r\n\r\nWklQ\r\n" +
	"--b--\r\n"

func run(t *testing.T, status int) (m *metrics.Metrics, uploads *atomic.Int32, sendErr error, token string) {
	t.Helper()
	var gotToken atomic.Value
	uploads = &atomic.Int32{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/documents/post_document/" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		gotToken.Store(r.Header.Get("Authorization"))
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		}
		f, h, err := r.FormFile("document")
		if err != nil {
			t.Errorf("document field: %v", err)
		} else {
			b, _ := io.ReadAll(f)
			if h.Filename != "a.pdf" || string(b) != "Hello" {
				t.Errorf("unexpected upload %q %q", h.Filename, b)
			}
		}
		uploads.Add(1)
		if status != http.StatusOK {
			http.Error(w, "nope", status)
			return
		}
		w.Write([]byte(`"11111111-2222-3333-4444-555555555555"`))
	}))
	t.Cleanup(api.Close)

	m = metrics.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := paperless.New(api.URL, "secret", 5*time.Second, 0, paperless.Options{}, m, log)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := smtpserver.New(smtpserver.Options{
		Domain: "localhost", MaxBytes: 1 << 20, MaxRcpts: 5, Timeout: 5 * time.Second,
		Whitelist: mailparse.NewWhitelist([]string{"pdf", "tiff", "jpg"}),
		UploadCtx: context.Background(), Uploader: client, Metrics: m, Logger: log,
	})
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	sendErr = smtp.SendMail(ln.Addr().String(), nil, "s@example.local", []string{"p@example.local"}, []byte(mail))
	tok, _ := gotToken.Load().(string)
	return m, uploads, sendErr, tok
}

func TestEndToEnd(t *testing.T) {
	m, uploads, err, token := run(t, http.StatusOK)
	if err != nil {
		t.Fatal(err)
	}
	if uploads.Load() != 1 {
		t.Errorf("uploads = %d, want 1 (zip must be skipped)", uploads.Load())
	}
	if token != "Token secret" {
		t.Errorf("Authorization = %q", token)
	}
	if got := testutil.ToFloat64(m.Attachments.WithLabelValues("uploaded")); got != 1 {
		t.Errorf("uploaded metric = %v", got)
	}
	if got := testutil.ToFloat64(m.Attachments.WithLabelValues("skipped")); got != 1 {
		t.Errorf("skipped metric = %v", got)
	}
}

func TestUploadFailureIsTemporarySMTPError(t *testing.T) {
	m, _, err, _ := run(t, http.StatusInternalServerError)
	if err == nil || !strings.HasPrefix(err.Error(), "451") {
		t.Fatalf("err = %v, want 451", err)
	}
	if got := testutil.ToFloat64(m.Attachments.WithLabelValues("failed")); got != 1 {
		t.Errorf("failed metric = %v", got)
	}
}
