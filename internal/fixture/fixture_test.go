package fixture

import (
	"testing"
)

func TestFixedFixturesLoad(t *testing.T) {
	specs, err := LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 3 {
		t.Fatalf("got %d specimens, want 3", len(specs))
	}
	want := map[string]int{
		"S1-STABLE":  9,
		"S2-ORIGIN":  10,
		"S3-REPLICA": 7, // 6 levels, 30 mT measured twice and both kept
	}
	for _, s := range specs {
		if len(s.Steps) != want[s.ID] {
			t.Fatalf("%s has %d steps, want %d", s.ID, len(s.Steps), want[s.ID])
		}
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	a := Generate()
	b := Generate()
	if len(a) != len(b) {
		t.Fatal("regeneration changed specimen count")
	}
	for i := range a {
		if a[i].ID != b[i].ID || len(a[i].Steps) != len(b[i].Steps) {
			t.Fatalf("regeneration mismatch for %s", a[i].ID)
		}
		for j := range a[i].Steps {
			x, y := a[i].Steps[j], b[i].Steps[j]
			if x.Key != y.Key || x.X != y.X || x.Y != y.Y || x.Z != y.Z ||
				x.Treatment != y.Treatment || x.Level != y.Level || x.Rep != y.Rep {
				t.Fatalf("non-deterministic step %s/%s: %+v vs %+v", a[i].ID, x.Key, x, y)
			}
		}
	}
}

func TestReplicatedLevelsDistinct(t *testing.T) {
	specs, _ := LoadAll()
	for _, s := range specs {
		if s.ID != "S3-REPLICA" {
			continue
		}
		keys := map[string]bool{}
		reps := 0
		for _, st := range s.Steps {
			if keys[st.Key] {
				t.Fatalf("duplicate key %s (possible silent merge)", st.Key)
			}
			keys[st.Key] = true
			if st.Level == 30 {
				reps++
			}
		}
		if reps != 2 {
			t.Fatalf("expected two retained 30 mT rows, got %d", reps)
		}
	}
}
