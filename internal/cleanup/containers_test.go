package cleanup

import (
	"reflect"
	"testing"
)

func TestBuildListContainersArgs(t *testing.T) {
	got := buildListContainersArgs()
	want := []string{"ps", "-a", "--format", "{{json .}}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildListContainersArgs() = %v, want %v", got, want)
	}
}

func TestParseContainer(t *testing.T) {
	line := `{"ID":"abc123","Names":"/myapp","Image":"tengiz-apps/myapp:production-v1","Labels":"tengiz-app=myapp,tengiz-env=production","State":"exited"}`
	c, err := parseContainer(line)
	if err != nil {
		t.Fatalf("parseContainer() error = %v", err)
	}
	if c.ID != "abc123" || c.State != "exited" {
		t.Errorf("parseContainer() = %+v", c)
	}
}

func TestIsStopped(t *testing.T) {
	cases := []struct {
		state string
		want  bool
	}{
		{"exited", true},
		{"created", true},
		{"dead", true},
		{"running", false},
		{"paused", false},
		{"restarting", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isStopped(tc.state); got != tc.want {
			t.Errorf("isStopped(%q) = %v, want %v", tc.state, got, tc.want)
		}
	}
}

func TestIsTengizManaged(t *testing.T) {
	cases := []struct {
		labels string
		want   bool
	}{
		{"tengiz-app=myapp", true},
		{"tengiz-env=production", true},
		{"tengiz-app=myapp,tengiz-env=staging", true},
		{"com.docker.compose.project=web", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isTengizManaged(tc.labels); got != tc.want {
			t.Errorf("isTengizManaged(%q) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}

func TestPartitionContainers(t *testing.T) {
	records := []stoppedContainer{
		{ID: "aaa", Names: "/running-app", Labels: "tengiz-app=runapp", State: "running"},
		{ID: "bbb", Names: "/stopped-app", Labels: "tengiz-app=myapp,tengiz-env=production", State: "exited"},
		{ID: "ccc", Names: "/created-app", Labels: "tengiz-app=myapp", State: "created"},
		{ID: "ddd", Names: "/junk-helper", Labels: "", State: "exited"},
		{ID: "eee", Names: "/paused-app", Labels: "", State: "paused"},
	}
	remove, keep := partitionContainers(records)

	wantRemove := []string{"ddd"}
	wantKeep := []string{"stopped-app", "created-app"}

	if got := names(remove); !reflect.DeepEqual(got, wantRemove) {
		t.Errorf("names(remove) = %v, want %v", got, wantRemove)
	}
	if got := names(keep); !reflect.DeepEqual(got, wantKeep) {
		t.Errorf("names(keep) = %v, want %v", got, wantKeep)
	}
}

func TestBuildRemoveContainersArgs(t *testing.T) {
	got := buildRemoveContainersArgs([]string{"ddd", "fff"})
	want := []string{"rm", "-f", "ddd", "fff"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildRemoveContainersArgs() = %v, want %v", got, want)
	}
}

func TestIdsAndNames(t *testing.T) {
	cs := []stoppedContainer{{ID: "abc", Names: "/foo"}, {ID: "", Names: ""}}
	if got := ids(cs); !reflect.DeepEqual(got, []string{"abc"}) {
		t.Errorf("ids() = %v", got)
	}
	if got := names(cs); !reflect.DeepEqual(got, []string{"foo", "abc"}) {
		t.Errorf("names() = %v", got)
	}
}