// Package paperless is a minimal client for the paperless-ngx document API.
package paperless

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Options controls metadata sent along with every uploaded document.
type Options struct {
	Tags          []string // tag IDs
	Correspondent string   // correspondent ID
	DocumentType  string   // document type ID
}

// Observer receives request outcomes; used for metrics. May be nil.
type Observer interface {
	ObserveRequest(code string)
}

// Client uploads documents to paperless-ngx.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	opts    Options
	retries int
	backoff time.Duration
	obs     Observer
	log     *slog.Logger
}

// New creates a client. retries is the number of additional attempts after the first.
func New(baseURL, token string, timeout time.Duration, retries int, opts Options, obs Observer, log *slog.Logger) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: timeout},
		opts:    opts,
		retries: retries,
		backoff: time.Second,
		obs:     obs,
		log:     log,
	}
}

// Upload posts a document to /api/documents/post_document/ and returns the
// consumption task ID reported by paperless-ngx.
func (c *Client) Upload(ctx context.Context, filename, contentType string, data []byte) (string, error) {
	body, ctHeaderValue, err := c.buildBody(filename, contentType, data)
	if err != nil {
		return "", err
	}

	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			delay := c.backoff << (attempt - 1)
			c.log.Warn("retrying upload", "file", filename, "attempt", attempt+1, "delay", delay, "error", lastErr)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		task, retryable, err := c.post(ctx, body, ctHeaderValue)
		if err == nil {
			return task, nil
		}
		lastErr = err
		if !retryable {
			break
		}
	}
	return "", lastErr
}

func (c *Client) buildBody(filename, contentType string, data []byte) ([]byte, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(filename))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="document"; filename=%s`, strconv.Quote(filename)))
	h.Set("Content-Type", contentType)
	fw, err := mw.CreatePart(h)
	if err != nil {
		return nil, "", err
	}
	if _, err := fw.Write(data); err != nil {
		return nil, "", err
	}

	for _, t := range c.opts.Tags {
		if err := mw.WriteField("tags", t); err != nil {
			return nil, "", err
		}
	}
	if c.opts.Correspondent != "" {
		if err := mw.WriteField("correspondent", c.opts.Correspondent); err != nil {
			return nil, "", err
		}
	}
	if c.opts.DocumentType != "" {
		if err := mw.WriteField("document_type", c.opts.DocumentType); err != nil {
			return nil, "", err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}

// post performs a single request. retryable is true for transient failures.
func (c *Client) post(ctx context.Context, body []byte, contentType string) (task string, retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/documents/post_document/", bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		c.observe("error")
		return "", true, fmt.Errorf("paperless request: %w", err)
	}
	defer resp.Body.Close()
	c.observe(strconv.Itoa(resp.StatusCode))

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		retry := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		return "", retry, fmt.Errorf("paperless returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	// On success the body is the task UUID as a JSON string.
	var id string
	if err := json.Unmarshal(respBody, &id); err != nil {
		id = strings.Trim(strings.TrimSpace(string(respBody)), `"`)
	}
	return id, false, nil
}

func (c *Client) observe(code string) {
	if c.obs != nil {
		c.obs.ObserveRequest(code)
	}
}
