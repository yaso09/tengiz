package runtime

import "testing"

func TestParseContainerLine(t *testing.T) {
	c, ok := parseContainerLine("abc123|tengiz-myapp|myapp|running")
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if c.id != "abc123" || c.name != "tengiz-myapp" || c.appLabel != "myapp" || !c.running {
		t.Errorf("got %+v", c)
	}
}

func TestParseContainerLineNoLabel(t *testing.T) {
	c, ok := parseContainerLine("xyz|nginx| |exited")
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if c.appLabel != "" || c.running {
		t.Errorf("got %+v, want empty label and not running", c)
	}
}

func TestParseContainerLineMalformed(t *testing.T) {
	if _, ok := parseContainerLine("only-one-field"); ok {
		t.Fatal("expected malformed line to fail parse")
	}
}

func TestSelectStaleContainers(t *testing.T) {
	lines := []dockerContainer{
		{name: "tengiz-app1", appLabel: "app1", running: true},             // running current -> keep
		{name: "tengiz-app1-1700000000", appLabel: "app1", running: false}, // stale deployment -> remove
		{name: "tengiz-app2", appLabel: "app2", running: false},            // stopped current -> protected
		{name: "nginx-junk", appLabel: "", running: false},                 // unmanaged stopped -> aggressive only
		{name: "tengiz-app3-pr-5", appLabel: "app3", running: false},       // stopped preview -> protected
	}
	protect := map[string]bool{
		"tengiz-app2":       true,
		"tengiz-app3-pr-5": true,
	}
	got := selectStaleContainers(lines, protect, false)
	if len(got) != 1 || got[0] != "tengiz-app1-1700000000" {
		t.Errorf("non-aggressive: got %v", got)
	}
	gotAgg := selectStaleContainers(lines, protect, true)
	want := []string{"tengiz-app1-1700000000", "nginx-junk"}
	if len(gotAgg) != len(want) {
		t.Fatalf("aggressive: got %v, want %v", gotAgg, want)
	}
	for i := range want {
		if gotAgg[i] != want[i] {
			t.Errorf("aggressive[%d] = %q, want %q", i, gotAgg[i], want[i])
		}
	}
}

func TestReclaimLines(t *testing.T) {
	out := []byte("Deleted Images:\nuntagged: foo\n\nTotal reclaimed space: 120.5MB\n")
	got := reclaimLines(out)
	if len(got) != 1 || got[0] != "Total reclaimed space: 120.5MB" {
		t.Fatalf("reclaimLines = %v", got)
	}
}

func TestSplitNonEmpty(t *testing.T) {
	got := splitNonEmpty([]byte("  a \n\nb\n \n"))
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("splitNonEmpty = %v", got)
	}
}

func TestEffectiveCleanupCategoriesDefault(t *testing.T) {
	c, i, v, n, k := effectiveCleanupCategories(CleanupOptions{})
	if !c || !i || v || n || !k {
		t.Errorf("default categories = %v %v %v %v %v, want true true false false true", c, i, v, n, k)
	}
}

func TestEffectiveCleanupCategoriesExplicit(t *testing.T) {
	c, i, v, n, k := effectiveCleanupCategories(CleanupOptions{Volumes: true})
	if c || i || !v || n || k {
		t.Errorf("volumes-only categories = %v %v %v %v %v, want false false true false false", c, i, v, n, k)
	}
}
