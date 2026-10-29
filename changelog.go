package main

import (
	_ "embed"
	"regexp"
	"strings"
)

// CHANGELOG.md is the single source for the in-app "What's new" pop-up and the
// GitHub release notes.
//
//go:embed CHANGELOG.md
var changelogMD string

type ChangeItem struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

type ChangeEntry struct {
	Version string       `json:"v"`
	Name    string       `json:"name"`
	Date    string       `json:"date"`
	From    string       `json:"from"`
	Items   []ChangeItem `json:"items"`
}

var (
	reHeading = regexp.MustCompile(`^##\s+(\d+\.\d+\.\d+)\s*(?:\((.*)\))?\s*$`)
	reBold    = regexp.MustCompile(`^\*\*(.+?)\*\*[:.]?\s*(.*)$`)
	reSince   = regexp.MustCompile(`(?i)^changes since (.+?)\.?$`)
)

// parseChangelog reads entries in the form
//
//	## 1.1.0 (29 October 2026)
//	Changes since Optic V1.0.3
//	- **Title**: what changed.
func parseChangelog(md string) []ChangeEntry {
	var out []ChangeEntry
	var cur *ChangeEntry
	for _, raw := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if m := reHeading.FindStringSubmatch(line); m != nil {
			out = append(out, ChangeEntry{Version: m[1], Name: appName + " V" + displayVersion(m[1]), Date: m[2]})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil || line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := reSince.FindStringSubmatch(line); m != nil {
			cur.From = m[1]
			continue
		}
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			body := strings.TrimSpace(line[2:])
			if m := reBold.FindStringSubmatch(body); m != nil {
				cur.Items = append(cur.Items, ChangeItem{Title: m[1], Text: m[2]})
			} else {
				cur.Items = append(cur.Items, ChangeItem{Title: body})
			}
		} else if n := len(cur.Items); n > 0 { // continuation line
			cur.Items[n-1].Text = strings.TrimSpace(cur.Items[n-1].Text + " " + line)
		}
	}
	return out
}

// releaseNotes returns the markdown section for one version (for GitHub).
func releaseNotes(md, version string) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var b strings.Builder
	in := false
	for _, l := range lines {
		if m := reHeading.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			if in {
				break
			}
			in = m[1] == version
			continue
		}
		if in {
			b.WriteString(l + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// displayVersion: 1.0.0 -> 1.0, 1.0.3 -> 1.0.3
func displayVersion(v string) string {
	return strings.TrimSuffix(v, ".0")
}
