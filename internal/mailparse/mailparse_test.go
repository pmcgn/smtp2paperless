package mailparse

import (
	"strings"
	"testing"
)

const sample = "From: scanner@example.local\r\n" +
	"To: paperless@example.local\r\n" +
	"Subject: scan\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=outer\r\n\r\n" +
	"--outer\r\n" +
	"Content-Type: text/plain; charset=us-ascii\r\n\r\n" +
	"hello\r\n" +
	"--outer\r\n" +
	"Content-Type: application/pdf; name=\"scan.PDF\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"Content-Disposition: attachment; filename=\"scan.PDF\"\r\n\r\n" +
	"SGVsbG8gUERG\r\n" +
	"--outer\r\n" +
	"Content-Type: image/jpeg\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"Content-Disposition: attachment; filename=\"=?UTF-8?B?c2Nhbi3DvC5qcGVn?=\"\r\n\r\n" +
	"SlBFRw==\r\n" +
	"--outer\r\n" +
	"Content-Type: application/zip\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"Content-Disposition: attachment; filename=\"../evil.zip\"\r\n\r\n" +
	"WklQ\r\n" +
	"--outer--\r\n"

func TestParseAndWhitelist(t *testing.T) {
	parts, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 {
		t.Fatalf("got %d parts, want 3: %+v", len(parts), parts)
	}
	if parts[0].Filename != "scan.PDF" || string(parts[0].Data) != "Hello PDF" {
		t.Errorf("part 0 = %q %q", parts[0].Filename, parts[0].Data)
	}
	if parts[1].Filename != "scan-ü.jpeg" || string(parts[1].Data) != "JPEG" {
		t.Errorf("part 1 = %q %q", parts[1].Filename, parts[1].Data)
	}
	if parts[2].Filename != "evil.zip" {
		t.Errorf("part 2 filename not sanitized: %q", parts[2].Filename)
	}

	wl := NewWhitelist([]string{"tiff", "pdf", "jpg"})
	want := []bool{true, true, false}
	for i, p := range parts {
		if got := wl.Allows(p); got != want[i] {
			t.Errorf("Allows(%q) = %v, want %v", p.Filename, got, want[i])
		}
	}
	if !NewWhitelist([]string{".TIF"}).Allows(Part{Filename: "a.tiff"}) {
		t.Error("tif/tiff alias not honoured")
	}
}
