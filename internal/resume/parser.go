// Package resume ingests a user's resume: it extracts plain text from PDF, DOCX,
// or TXT (the deterministic fact set the no-fabrication check later relies on)
// and asks Claude to organize that text into a structured profile for matching.
package resume

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ErrUnreadable indicates the resume could not be parsed into usable text.
var ErrUnreadable = fmt.Errorf("resume is empty or could not be read")

// Format enumerates the accepted resume file formats.
const (
	FormatPDF  = "pdf"
	FormatDOCX = "docx"
	FormatTXT  = "txt"
)

// DetectFormat maps a filename to a supported format, or "" when unsupported.
func DetectFormat(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf":
		return FormatPDF
	case ".docx":
		return FormatDOCX
	case ".txt", ".text", ".md":
		return FormatTXT
	default:
		return ""
	}
}

// ExtractText returns the plain text of a resume in the given format. It rejects
// empty or unreadable input with ErrUnreadable (FR-018).
func ExtractText(format string, data []byte) (string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return "", ErrUnreadable
	}
	var (
		text string
		err  error
	)
	switch format {
	case FormatPDF:
		text, err = extractPDF(data)
	case FormatDOCX:
		text, err = extractDOCX(data)
	case FormatTXT:
		text = string(data)
	default:
		return "", fmt.Errorf("unsupported resume format %q", format)
	}
	if err != nil {
		return "", fmt.Errorf("extract %s: %w", format, err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ErrUnreadable
	}
	return text, nil
}

func extractPDF(data []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	plain, err := reader.GetPlainText()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, plain); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// docxTextPattern captures the text runs (<w:t>…</w:t>) inside a Word document.
var docxTextPattern = regexp.MustCompile(`(?s)<w:t[^>]*>(.*?)</w:t>`)

// paragraphBreak marks the end of a Word paragraph so extracted text keeps line
// structure.
var paragraphBreak = regexp.MustCompile(`</w:p>`)

func extractDOCX(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	var docXML []byte
	for _, file := range zr.File {
		if file.Name == "word/document.xml" {
			rc, err := file.Open()
			if err != nil {
				return "", err
			}
			docXML, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return "", err
			}
			break
		}
	}
	if docXML == nil {
		return "", fmt.Errorf("word/document.xml not found")
	}

	// Insert newlines at paragraph boundaries, then pull the text runs.
	withBreaks := paragraphBreak.ReplaceAll(docXML, []byte("</w:p>\n"))
	var b strings.Builder
	for _, match := range docxTextPattern.FindAllSubmatch(withBreaks, -1) {
		b.WriteString(html.UnescapeString(string(match[1])))
	}
	return b.String(), nil
}
