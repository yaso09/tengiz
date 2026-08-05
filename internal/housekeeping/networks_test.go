package housekeeping

import (
	"context"
	"reflect"
	"testing"
)

func TestDanglingNetworksReturnsIDs(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{
		"network ls -q -f dangling=true": "net_aaa\nnet_bbb\n",
	})
	m := NewManager(runner)
	got, err := m.danglingNetworks(context.Background())
	if err != nil {
		t.Fatalf("danglingNetworks() error = %v", err)
	}
	if want := []string{"net_aaa", "net_bbb"}; !reflect.DeepEqual(got, want) {
		t.Errorf("danglingNetworks() = %v, want %v", got, want)
	}
}
