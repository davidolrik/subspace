package cmd

import (
	"bytes"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/sblinch/kdl-go"
	"github.com/sblinch/kdl-go/document"
)

// TestSchemaCommandPrintsEmbeddedKDL checks the basic plumbing: the
// `subspace schema` command writes the embedded schema to stdout, and
// the output is non-empty and starts with the comment header.
func TestSchemaCommandPrintsEmbeddedKDL(t *testing.T) {
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"schema"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("schema output is empty")
	}
	if !strings.Contains(out.String(), "subspace.kdl-schema") {
		t.Errorf("output missing header comment\n%s", out.String())
	}
}

// TestSchemaIsParseableKDL ensures the embedded schema is itself a
// well-formed KDL document, so we never ship a broken schema that
// editors would refuse to load.
func TestSchemaIsParseableKDL(t *testing.T) {
	if _, err := kdl.Parse(bytes.NewReader(kdlSchemaContent)); err != nil {
		t.Fatalf("schema fails to parse as KDL: %v", err)
	}
}

// TestSchemaDocsCopyIsInSync ensures the canonical schema embedded in
// the binary matches the copy under docs/public/ that VitePress
// publishes at https://subspace.pub/subspace.kdl-schema. A diff here
// means either copy was edited without the other; fix by copying:
//
//   cp cmd/subspace.kdl-schema docs/public/subspace.kdl-schema
func TestSchemaDocsCopyIsInSync(t *testing.T) {
	publicCopy, err := os.ReadFile("../docs/public/subspace.kdl-schema")
	if err != nil {
		t.Fatalf("reading docs/public copy: %v (run from repo root with `go test ./cmd/...`)", err)
	}
	if !bytes.Equal(publicCopy, kdlSchemaContent) {
		t.Error("docs/public/subspace.kdl-schema differs from embedded cmd/subspace.kdl-schema; copy the embedded version over the public one")
	}
}

// TestSchemaCoversAllTopLevelNodes is the drift detector: it asserts
// the schema describes every top-level node the config parser
// recognises today. If a new node is added to config.go's switch
// statement without updating the schema, this test fails.
func TestSchemaCoversAllTopLevelNodes(t *testing.T) {
	// Top-level node names the parser accepts. Keep this in sync
	// with the switch statement in config/config.go's parseData.
	want := []string{
		"control_socket",
		"include",
		"listen",
		"page",
		"route",
		"search-engines",
		"stats",
		"tags",
		"upstream",
	}

	doc, err := kdl.Parse(bytes.NewReader(kdlSchemaContent))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}

	var schemaDoc *document.Node
	for _, n := range doc.Nodes {
		if n.Name.ValueString() == "document" {
			schemaDoc = n
			break
		}
	}
	if schemaDoc == nil {
		t.Fatal("schema is missing the top-level `document` block")
	}

	got := map[string]bool{}
	for _, child := range schemaDoc.Children {
		if child.Name.ValueString() != "node" {
			continue
		}
		if len(child.Arguments) == 0 {
			continue
		}
		got[child.Arguments[0].ValueString()] = true
	}

	var missing []string
	for _, name := range want {
		if !got[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("schema is missing top-level nodes: %s", strings.Join(missing, ", "))
	}
}
