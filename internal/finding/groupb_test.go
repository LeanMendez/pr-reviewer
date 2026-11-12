package finding

import "testing"

func TestFilterGroupB(t *testing.T) {
	fs := []Finding{
		{ID: "a1", Group: GroupA, Status: StatusConfirmado, File: "a.go"},
		{ID: "a2", Group: GroupA, Status: StatusConfirmado, File: "b.go", Function: "F"},
		{ID: "a3", Group: GroupA, Status: StatusRefutado, File: "r.go", Function: "R"},
		{ID: "b-same-file", Group: GroupB, File: "a.go"},
		{ID: "b-same-func", Group: GroupB, File: "b.go", Function: "F"},
		{ID: "b-nothing", Group: GroupB, File: "c.go"},
		{ID: "b-refuted-a", Group: GroupB, File: "r.go", Function: "R"},
	}
	var got []string
	for _, f := range FilterGroupB(fs) {
		got = append(got, f.ID)
	}
	want := []string{"a1", "a2", "a3", "b-same-file", "b-same-func"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
