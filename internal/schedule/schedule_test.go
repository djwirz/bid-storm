package schedule

import (
	"path/filepath"
	"reflect"
	"testing"
)

var params = Params{Seed: 7, Rate: 200, DurationUS: 10_000_000, Bidders: 20, RetryRate: 0.1, ConflictRate: 0.3}

func generate(t *testing.T, p Params) Schedule {
	t.Helper()
	s, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSameSeedSameSchedule(t *testing.T) {
	if !reflect.DeepEqual(generate(t, params), generate(t, params)) {
		t.Fatal("same params gave different schedules")
	}
	other := params
	other.Seed = 8
	if reflect.DeepEqual(generate(t, params).Entries, generate(t, other).Entries) {
		t.Fatal("different seeds gave the same entries")
	}
}

func TestShape(t *testing.T) {
	s := generate(t, params)
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	// 2000 expected; a Poisson count stays well within 10% of that, and the seed is fixed.
	if n := len(s.Entries); n < 1800 || n > 2200 {
		t.Fatalf("%d entries, expected about 2000", n)
	}
	retries, conflicts := 0, 0
	for _, e := range s.Entries {
		if e.OffsetUS < 0 || e.OffsetUS >= params.DurationUS {
			t.Fatalf("offset %d outside the storm", e.OffsetUS)
		}
		if e.RetryOf != nil {
			retries++
			if e.ChangeAmount {
				conflicts++
			}
		} else if e.ChangeAmount {
			t.Fatal("a first send can't be a changed retry")
		}
	}
	if retries == 0 || conflicts == 0 || conflicts == retries {
		t.Fatalf("retries %d, conflicts %d", retries, conflicts)
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedule.json")
	s := generate(t, params)
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, got) {
		t.Fatal("round trip changed the schedule")
	}
}

func TestRejectsBadInput(t *testing.T) {
	for name, change := range map[string]func(*Params){
		"rate":     func(p *Params) { p.Rate = 0 },
		"duration": func(p *Params) { p.DurationUS = 0 },
		"bidders":  func(p *Params) { p.Bidders = 0 },
		"retry":    func(p *Params) { p.RetryRate = 1.5 },
		"too big":  func(p *Params) { p.Rate = 1e9 },
	} {
		p := params
		change(&p)
		if _, err := Generate(p); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	s := generate(t, params)
	bad := 0
	s.Entries[0].RetryOf = &bad // retries itself
	if s.Validate() == nil {
		t.Error("a self-retry validated")
	}
}
