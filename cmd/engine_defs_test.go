package cmd

import (
	"testing"

	"go.olrik.dev/subspace/config"
)

// engineDefs is a thin field-copy from config.SearchEngine to
// pages.SearchEngineDef. The integration is covered separately by
// the config parser tests and the handler tests; this exercises the
// converter so a future drift between the two structs is caught here.
func TestEngineDefs(t *testing.T) {
	const input = `
search-engines default="google" {
	engine "google"   url="https://www.google.com/search?q={query}" icon="si-google" alias="g" description="Web search"
	engine "metacpan" url="https://metacpan.org/search?q={query}"   alias="cpan"
}
`
	cfg, err := config.Parse([]byte(input))
	if err != nil {
		t.Fatalf("config.Parse failed: %v", err)
	}

	defs := engineDefs(cfg)
	if len(defs) != 2 {
		t.Fatalf("got %d defs, want 2", len(defs))
	}

	g, ok := defs["google"]
	if !ok {
		t.Fatal("missing google in defs")
	}
	if g.Name != "google" || g.Alias != "g" || g.Icon != "si-google" || g.Description != "Web search" {
		t.Errorf("google def = %+v, fields not copied through", g)
	}
	if g.URL != "https://www.google.com/search?q={query}" {
		t.Errorf("google URL = %q, want template preserved", g.URL)
	}

	if cpan := defs["metacpan"]; cpan.Alias != "cpan" {
		t.Errorf("metacpan.Alias = %q, want %q", cpan.Alias, "cpan")
	}
}
