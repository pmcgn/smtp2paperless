// Package mailparse extracts file attachments from raw RFC 5322 messages.
package mailparse

import (
	"fmt"
	"io"
	"mime"
	"path"
	"strings"

	"github.com/emersion/go-message"
)

// Part is a single decoded attachment.
type Part struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Ext returns the normalized (lowercase, alias-folded) extension of the part's
// filename without the leading dot, or "" if there is none.
func (p Part) Ext() string {
	return normalizeExt(strings.TrimPrefix(path.Ext(p.Filename), "."))
}

// Parse reads a raw message and returns every attachment-like leaf part in
// document order. Transfer encodings (base64, quoted-printable) are decoded.
// Plain text/HTML body parts without a filename are not returned.
func Parse(r io.Reader) ([]Part, error) {
	e, err := message.Read(r)
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	var parts []Part
	if err := walk(e, &parts); err != nil {
		return nil, err
	}
	return parts, nil
}

func walk(e *message.Entity, out *[]Part) error {
	if mr := e.MultipartReader(); mr != nil {
		for {
			child, err := mr.NextPart()
			if err == io.EOF {
				return nil
			}
			if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
				return fmt.Errorf("read multipart: %w", err)
			}
			if err := walk(child, out); err != nil {
				return err
			}
		}
	}

	ctype, ctParams, _ := e.Header.ContentType()
	disp, dispParams, _ := e.Header.ContentDisposition()

	name := decodeWords(dispParams["filename"])
	if name == "" {
		name = decodeWords(ctParams["name"])
	}
	isBody := name == "" && disp != "attachment" && (ctype == "" || strings.HasPrefix(ctype, "text/"))
	if isBody {
		return nil
	}

	data, err := io.ReadAll(e.Body)
	if err != nil {
		return fmt.Errorf("read attachment %q: %w", name, err)
	}
	if name == "" {
		name = fmt.Sprintf("attachment-%d%s", len(*out)+1, extForType(ctype))
	}
	*out = append(*out, Part{Filename: sanitize(name), ContentType: ctype, Data: data})
	return nil
}

func decodeWords(s string) string {
	if s == "" {
		return ""
	}
	if d, err := new(mime.WordDecoder).DecodeHeader(s); err == nil {
		return d
	}
	return s
}

// sanitize strips any directory components and control characters.
func sanitize(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if name == "." || name == "/" || name == "" {
		return "attachment"
	}
	return name
}

var typeExt = map[string]string{
	"application/pdf": ".pdf",
	"image/jpeg":      ".jpg",
	"image/jpg":       ".jpg",
	"image/tiff":      ".tiff",
	"image/png":       ".png",
}

func extForType(ctype string) string { return typeExt[ctype] }

var extAlias = map[string]string{"jpeg": "jpg", "tif": "tiff"}

func normalizeExt(ext string) string {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	if a, ok := extAlias[ext]; ok {
		return a
	}
	return ext
}

// Whitelist decides which attachments are uploaded, based on file extension.
// "jpg"/"jpeg" and "tif"/"tiff" are treated as equivalent.
type Whitelist map[string]struct{}

// NewWhitelist builds a Whitelist from extensions such as "pdf" or ".TIFF".
func NewWhitelist(exts []string) Whitelist {
	w := Whitelist{}
	for _, e := range exts {
		if e = normalizeExt(strings.TrimSpace(e)); e != "" {
			w[e] = struct{}{}
		}
	}
	return w
}

// Allows reports whether the part's extension is whitelisted.
func (w Whitelist) Allows(p Part) bool {
	_, ok := w[p.Ext()]
	return ok
}
