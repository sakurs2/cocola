package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestPlainBannerContainsNoANSI(t *testing.T) {
	var output bytes.Buffer
	Printer{Out: &output, Err: &output}.Banner()
	if !strings.Contains(output.String(), "██████") {
		t.Fatalf("banner missing: %s", output.String())
	}
	if !strings.Contains(output.String(), "Your trusty & powerful agent platform") {
		t.Fatalf("banner tagline missing: %s", output.String())
	}
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("plain banner contains ANSI: %q", output.String())
	}
}

func TestAutoColorIsOffForBuffer(t *testing.T) {
	if AutoColor(&bytes.Buffer{}, false) {
		t.Fatal("buffer must not enable color")
	}
}

func TestPlainCommandAndPathRemainReadable(t *testing.T) {
	var output bytes.Buffer
	printer := Printer{Out: &output, Err: &output}
	printer.Path("/tmp/cocola/config.env")
	printer.Command("cocola start")
	if got := output.String(); !strings.Contains(got, "/tmp/cocola/config.env") ||
		!strings.Contains(got, "$ cocola start") || strings.Contains(got, "\x1b[") {
		t.Fatalf("plain output = %q", got)
	}
}
