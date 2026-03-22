package pages

import (
	"strings"
	"testing"
)

func TestErrorPageContainsStatusAndTitle(t *testing.T) {
	page := ErrorPage(502, "Dial Failed", "connection refused: proxy.corp.com:3128")

	s := string(page)
	if !strings.Contains(s, "502") {
		t.Error("error page missing status code")
	}
	if !strings.Contains(s, "Dial Failed") {
		t.Error("error page missing title")
	}
	if !strings.Contains(s, "connection refused: proxy.corp.com:3128") {
		t.Error("error page missing detail")
	}
}

func TestErrorPageIsValidHTTP(t *testing.T) {
	page := ErrorPage(502, "Bad Gateway", "upstream unreachable")

	s := string(page)
	if !strings.HasPrefix(s, "HTTP/1.1 502") {
		t.Errorf("expected HTTP response, got: %q", s[:min(len(s), 40)])
	}
	if !strings.Contains(s, "Content-Type: text/html") {
		t.Error("missing Content-Type header")
	}
	if !strings.Contains(s, "<!DOCTYPE html>") {
		t.Error("missing HTML doctype in body")
	}
}

func TestErrorPageEscapesHTML(t *testing.T) {
	page := ErrorPage(400, "Bad Request", `<script>alert("xss")</script>`)

	s := string(page)
	if strings.Contains(s, "<script>alert") {
		t.Error("error page did not escape HTML in detail")
	}
}

func TestErrorPage400(t *testing.T) {
	page := ErrorPage(400, "Bad Request", "missing Host header")

	s := string(page)
	if !strings.HasPrefix(s, "HTTP/1.1 400") {
		t.Errorf("expected 400 status line, got: %q", s[:min(len(s), 40)])
	}
}
