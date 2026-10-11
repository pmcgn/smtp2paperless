// Package smtpserver receives mail over plain SMTP and forwards whitelisted
// attachments to an Uploader.
package smtpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/emersion/go-smtp"

	"github.com/pmcgn/smtp2paperless/internal/mailparse"
	"github.com/pmcgn/smtp2paperless/internal/metrics"
)

// Uploader stores a document and returns an identifier for logging.
type Uploader interface {
	Upload(ctx context.Context, filename, contentType string, data []byte) (string, error)
}

// Options configures the SMTP server.
type Options struct {
	Addr      string
	Domain    string
	MaxBytes  int64
	MaxRcpts  int
	Timeout   time.Duration
	Whitelist mailparse.Whitelist
	UploadCtx context.Context
	Uploader  Uploader
	Metrics   *metrics.Metrics
	Logger    *slog.Logger
}

// New builds a configured (not yet listening) SMTP server.
func New(o Options) *smtp.Server {
	be := &backend{o: o}
	s := smtp.NewServer(be)
	s.Addr = o.Addr
	s.Domain = o.Domain
	s.MaxMessageBytes = o.MaxBytes
	s.MaxRecipients = o.MaxRcpts
	s.ReadTimeout = o.Timeout
	s.WriteTimeout = o.Timeout
	// Plain SMTP without authentication or TLS: the sender is a trusted scanner.
	s.AllowInsecureAuth = true
	s.ErrorLog = slogAdapter{o.Logger}
	return s
}

type backend struct{ o Options }

func (b *backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	remote := ""
	if nc := c.Conn(); nc != nil {
		remote = nc.RemoteAddr().String()
	}
	b.o.Metrics.SMTPConnections.Inc()
	b.o.Metrics.SMTPActiveConns.Inc()
	return &session{
		o:      b.o,
		remote: remote,
		log:    b.o.Logger.With("remote", remote),
	}, nil
}

type session struct {
	o      Options
	remote string
	log    *slog.Logger
	from   string
	rcpts  []string
}

func (s *session) Reset() {
	s.from = ""
	s.rcpts = nil
}

func (s *session) Logout() error {
	s.o.Metrics.SMTPActiveConns.Dec()
	return nil
}

func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	s.from = from
	s.log.Debug("MAIL FROM", "from", from)
	return nil
}

func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.rcpts = append(s.rcpts, to)
	s.log.Debug("RCPT TO", "to", to)
	return nil
}

func (s *session) Data(r io.Reader) error {
	log := s.log.With("from", s.from, "rcpts", s.rcpts)
	m := s.o.Metrics

	parts, err := mailparse.Parse(r)
	if err != nil {
		m.Messages.WithLabelValues("rejected").Inc()
		log.Error("cannot parse message", "error", err)
		return &smtp.SMTPError{Code: 554, EnhancedCode: smtp.EnhancedCode{5, 6, 0}, Message: "cannot parse message"}
	}
	log.Info("message received", "attachments", len(parts))

	var failed int
	var uploaded int
	for _, p := range parts {
		plog := log.With("file", p.Filename, "type", p.ContentType, "bytes", len(p.Data))
		if !s.o.Whitelist.Allows(p) {
			m.Attachments.WithLabelValues("skipped").Inc()
			plog.Info("attachment skipped: extension not whitelisted")
			continue
		}
		if len(p.Data) == 0 {
			m.Attachments.WithLabelValues("skipped").Inc()
			plog.Warn("attachment skipped: empty")
			continue
		}

		start := time.Now()
		task, err := s.o.Uploader.Upload(s.o.UploadCtx, p.Filename, p.ContentType, p.Data)
		m.UploadDuration.Observe(time.Since(start).Seconds())
		if err != nil {
			failed++
			m.Attachments.WithLabelValues("failed").Inc()
			plog.Error("upload failed", "error", err)
			continue
		}
		uploaded++
		m.Attachments.WithLabelValues("uploaded").Inc()
		m.UploadedBytes.Add(float64(len(p.Data)))
		plog.Info("attachment uploaded", "task", task)
	}

	if failed > 0 {
		m.Messages.WithLabelValues("rejected").Inc()
		// 451 makes the sender treat this as temporary; paperless-ngx rejects
		// duplicates by checksum, so a retry does not create double documents.
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 3, 0},
			Message:      fmt.Sprintf("failed to upload %d attachment(s) to paperless-ngx", failed),
		}
	}
	if uploaded == 0 {
		log.Warn("message contained no whitelisted attachments")
	}
	m.Messages.WithLabelValues("accepted").Inc()
	return nil
}

// slogAdapter routes go-smtp's internal error log to slog.
type slogAdapter struct{ l *slog.Logger }

func (a slogAdapter) Printf(format string, v ...any) { a.l.Warn(fmt.Sprintf(format, v...)) }
func (a slogAdapter) Println(v ...any)               { a.l.Warn(fmt.Sprint(v...)) }

// IsClosed reports whether err is the expected result of shutting the server down.
func IsClosed(err error) bool { return errors.Is(err, smtp.ErrServerClosed) }
