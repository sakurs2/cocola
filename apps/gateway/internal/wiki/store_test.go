package wiki

import "testing"

func TestNormalizeName(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"Page.md", "研发资料", "Q3 plan.pptx"} {
		got, err := NormalizeName(value)
		if err != nil || got != value {
			t.Errorf("NormalizeName(%q) = %q, %v", value, got, err)
		}
	}
	for _, value := range []string{"", ".", "..", "a/b", `a\b`, "line\nbreak"} {
		if got, err := NormalizeName(value); err == nil {
			t.Errorf("NormalizeName(%q) = %q, want error", value, got)
		}
	}
}

func TestPopulateLogicalPaths(t *testing.T) {
	t.Parallel()
	nodes := []Node{
		{ID: "file", ParentID: "team", Name: "brief.md"},
		{ID: "root", Name: "Product"},
		{ID: "team", ParentID: "root", Name: "Roadmap"},
	}
	got := PopulateLogicalPaths(nodes)
	paths := make(map[string]string, len(got))
	for _, node := range got {
		paths[node.ID] = node.LogicalPath
	}
	if paths["root"] != "Product" ||
		paths["team"] != "Product/Roadmap" ||
		paths["file"] != "Product/Roadmap/brief.md" {
		t.Fatalf("PopulateLogicalPaths() = %#v", paths)
	}
	if nodes[0].LogicalPath != "" {
		t.Fatal("PopulateLogicalPaths must not mutate its input slice")
	}
}
