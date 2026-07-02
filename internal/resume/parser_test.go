package resume

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestDetectFormat(t *testing.T) {
	cases := map[string]string{
		"cv.pdf": FormatPDF, "resume.DOCX": FormatDOCX, "notes.txt": FormatTXT, "photo.png": "",
	}
	for name, want := range cases {
		if got := DetectFormat(name); got != want {
			t.Errorf("DetectFormat(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestExtractTextTXT(t *testing.T) {
	got, err := ExtractText(FormatTXT, []byte("Jane Doe\nGo engineer"))
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(got, "Go engineer") {
		t.Errorf("got %q, want it to contain the resume text", got)
	}
}

func TestExtractTextRejectsEmpty(t *testing.T) {
	if _, err := ExtractText(FormatTXT, []byte("   \n\t ")); err != ErrUnreadable {
		t.Errorf("err = %v, want ErrUnreadable", err)
	}
}

func TestExtractTextDOCX(t *testing.T) {
	docx := buildDOCX(t, `<w:document><w:body>`+
		`<w:p><w:r><w:t>Jane Doe</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t xml:space="preserve">Senior Go Engineer</w:t></w:r></w:p>`+
		`</w:body></w:document>`)

	got, err := ExtractText(FormatDOCX, docx)
	if err != nil {
		t.Fatalf("ExtractText(docx): %v", err)
	}
	if !strings.Contains(got, "Jane Doe") || !strings.Contains(got, "Senior Go Engineer") {
		t.Errorf("got %q, want both runs extracted", got)
	}
}

// buildDOCX zips a minimal document.xml into a .docx byte payload for tests.
func buildDOCX(t *testing.T, documentXML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write([]byte(documentXML)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}
