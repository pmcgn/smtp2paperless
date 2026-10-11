// Command mockpaperless is a stand-in for the paperless-ngx upload endpoint,
// for local development without a real paperless instance.
package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	addr := flag.String("addr", ":8000", "listen address")
	token := flag.String("token", "", "expected API token (empty: accept any)")
	dir := flag.String("save", "", "directory to save received documents to (optional)")
	flag.Parse()

	http.HandleFunc("POST /api/documents/post_document/", func(w http.ResponseWriter, r *http.Request) {
		if *token != "" && r.Header.Get("Authorization") != "Token "+*token {
			http.Error(w, `{"detail":"Invalid token."}`, http.StatusUnauthorized)
			return
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f, hdr, err := r.FormFile("document")
		if err != nil {
			http.Error(w, `{"document":["No file was submitted."]}`, http.StatusBadRequest)
			return
		}
		defer f.Close()
		n := int64(0)
		if *dir != "" {
			out, err := os.Create(filepath.Join(*dir, filepath.Base(hdr.Filename)))
			if err == nil {
				n, _ = io.Copy(out, f)
				out.Close()
			}
		} else {
			n, _ = io.Copy(io.Discard, f)
		}
		b := make([]byte, 16)
		rand.Read(b)
		task := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
		log.Printf("received %q (%s, %d bytes) tags=%v correspondent=%v document_type=%v -> task %s",
			hdr.Filename, hdr.Header.Get("Content-Type"), n,
			r.MultipartForm.Value["tags"], r.MultipartForm.Value["correspondent"], r.MultipartForm.Value["document_type"], task)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(task)
	})

	log.Println("mock paperless-ngx listening on", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
