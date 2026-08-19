package runtime

import "testing"

func TestCleanupContainerArgsProtectsTengiz(t *testing.T) {
	args := cleanupContainerArgs()
	found := false
	for _, a := range args {
		if a == "label!=tengiz-app" {
			found = true
		}
	}
	if !found {
		t.Fatalf("container prune args must exclude tengiz-app label, got %v", args)
	}
}

func TestCleanupContainerArgsPrefix(t *testing.T) {
	args := cleanupContainerArgs()
	want := []string{"container", "prune", "-f"}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("cleanupContainerArgs()[%d] = %q, want %q (full: %v)", i, args[i], want[i], args)
		}
	}
}

func TestCleanupImageArgs(t *testing.T) {
	args := cleanupImageArgs()
	want := []string{"image", "prune", "-f"}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("cleanupImageArgs()[%d] = %q, want %q (full: %v)", i, args[i], want[i], args)
		}
	}
}

func TestCleanupVolumeArgs(t *testing.T) {
	args := cleanupVolumeArgs()
	if len(args) != 3 || args[0] != "volume" || args[1] != "prune" || args[2] != "-f" {
		t.Fatalf("cleanupVolumeArgs() = %v", args)
	}
}

func TestCleanupNetworkArgs(t *testing.T) {
	args := cleanupNetworkArgs()
	if len(args) != 3 || args[0] != "network" || args[1] != "prune" || args[2] != "-f" {
		t.Fatalf("cleanupNetworkArgs() = %v", args)
	}
}

func TestCleanupCacheArgs(t *testing.T) {
	args := cleanupCacheArgs()
	if len(args) != 3 || args[0] != "builder" || args[1] != "prune" || args[2] != "-f" {
		t.Fatalf("cleanupCacheArgs() = %v", args)
	}
}

func TestListContainerArgsProtectsTengiz(t *testing.T) {
	args := listContainerArgs()
	if args[0] != "ps" || args[1] != "-a" {
		t.Fatalf("listContainerArgs() prefix = %v", args)
	}
	for _, a := range args {
		if a == "label!=tengiz-app" {
			return
		}
	}
	t.Fatalf("listContainerArgs() must exclude tengiz-app label, got %v", args)
}

func TestListDanglingImageArgs(t *testing.T) {
	args := listDanglingImageArgs()
	if args[0] != "images" {
		t.Fatalf("listDanglingImageArgs()[0] = %q, want %q", args[0], "images")
	}
}

func TestListDanglingVolumeArgs(t *testing.T) {
	args := listDanglingVolumeArgs()
	if args[0] != "volume" || args[1] != "ls" {
		t.Fatalf("listDanglingVolumeArgs() prefix = %v", args)
	}
}

func TestListDanglingNetworkArgs(t *testing.T) {
	args := listDanglingNetworkArgs()
	if args[0] != "network" || args[1] != "ls" {
		t.Fatalf("listDanglingNetworkArgs() prefix = %v", args)
	}
}

func TestCacheUsageArgs(t *testing.T) {
	args := cacheUsageArgs()
	if len(args) != 2 || args[0] != "system" || args[1] != "df" {
		t.Fatalf("cacheUsageArgs() = %v", args)
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	tests := []struct {
		output string
		want   uint64
	}{
		{"Total reclaimed space: 0B", 0},
		{"Total reclaimed space: 1.5KB", 1500},
		{"Total reclaimed space: 12.34MB", 12340000},
		{"Total reclaimed space: 2GB", 2000000000},
		{"Deleted Containers:\nabc\n\nTotal reclaimed space: 5.321MB\n", 5321000},
		{"no match here", 0},
		{"Total reclaimed space: ", 0},
	}
	for _, tt := range tests {
		if got := parseReclaimedBytes(tt.output); got != tt.want {
			t.Errorf("parseReclaimedBytes(%q) = %d, want %d", tt.output, got, tt.want)
		}
	}
}