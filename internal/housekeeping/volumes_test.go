package housekeeping

import (
	"context"
	"reflect"
	"testing"
)

func TestDanglingVolumesReturnsNames(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{
		"volume ls -q -f dangling=true": "vol_abc\nvol_def\n",
	})
	m := NewManager(runner)
	got, err := m.danglingVolumes(context.Background())
	if err != nil {
		t.Fatalf("danglingVolumes() error = %v", err)
	}
	if want := []string{"vol_abc", "vol_def"}; !reflect.DeepEqual(got, want) {
		t.Errorf("danglingVolumes() = %v, want %v", got, want)
	}
}
