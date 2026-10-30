package main

import "testing"

func TestChangelogParses(t *testing.T) {
	e := parseChangelog(changelogMD)
	if len(e) < 5 {
		t.Fatalf("expected entries, got %d", len(e))
	}
	if e[0].Version != appVersion {
		t.Fatalf("CHANGELOG.md's newest section (%s) must match appVersion (%s)", e[0].Version, appVersion)
	}
	for _, x := range e {
		if len(x.Items) == 0 || x.Date == "" {
			t.Fatalf("section %s is missing items or a date", x.Version)
		}
		for _, it := range x.Items {
			if it.Title == "" || it.Text == "" {
				t.Fatalf("section %s has an item without title/text: %+v", x.Version, it)
			}
		}
	}
	first := e[len(e)-1]
	if first.Version != "1.0.0" || first.Name != "Optic V1.0" || first.From != "ClearRide 2.0.1" {
		t.Fatalf("bad parse of 1.0.0: %+v", first)
	}
	if releaseNotes(changelogMD, "1.0.2") == "" {
		t.Fatal("no notes for 1.0.2")
	}
}
