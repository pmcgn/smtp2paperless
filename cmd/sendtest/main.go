// Command sendtest sends a test email with file attachments over plain SMTP,
// imitating a network scanner. Usage: sendtest [-addr host:port] file...
package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"mime"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
)

func main() {
	addr := flag.String("addr", "localhost:2525", "SMTP server address")
	from := flag.String("from", "scanner@example.local", "envelope sender")
	to := flag.String("to", "paperless@example.local", "envelope recipient")
	subject := flag.String("subject", "Scan from test device", "subject")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: sendtest [flags] file...")
		os.Exit(2)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fmt.Fprintf(&buf, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%s\r\n\r\n",
		*from, *to, *subject, mw.Boundary())

	h := textproto.MIMEHeader{}
	h.Set("Content-Type", "text/plain; charset=utf-8")
	w, _ := mw.CreatePart(h)
	fmt.Fprint(w, "Attached scanned document.\r\n")

	for _, f := range flag.Args() {
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		name := filepath.Base(f)
		ct := mime.TypeByExtension(filepath.Ext(name))
		if ct == "" {
			ct = "application/octet-stream"
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", fmt.Sprintf("%s; name=%q", ct, name))
		h.Set("Content-Transfer-Encoding", "base64")
		h.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		w, _ := mw.CreatePart(h)
		enc := base64.StdEncoding.EncodeToString(data)
		for len(enc) > 76 {
			fmt.Fprintf(w, "%s\r\n", enc[:76])
			enc = enc[76:]
		}
		fmt.Fprintf(w, "%s\r\n", enc)
	}
	mw.Close()

	if err := smtp.SendMail(*addr, nil, *from, []string{*to}, buf.Bytes()); err != nil {
		fmt.Fprintln(os.Stderr, "send failed:", err)
		os.Exit(1)
	}
	fmt.Println("sent", flag.NArg(), "attachment(s) to", *addr)
}
