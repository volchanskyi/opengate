package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// contentsItemPattern is one entry of a contents list: a bullet whose whole
// text is a link to an anchor on the same page.
var contentsItemPattern = regexp.MustCompile(`^( *)[-*] \[(.+)\]\(#([^)\s]+)\)[ \t]*$`)

const contentsRule = "a page with sections starts, right after its title, with a list linking every ## to #### heading in order"

// contentsEntry is one item of a page's opening contents list.
type contentsEntry struct {
	Line   int
	Indent int
	Text   string
	Anchor string
}

// needsContents reports whether a docs/ page other than a decision record needs a contents list.
func needsContents(path string) bool {
	return strings.HasPrefix(path, "docs/") &&
		!strings.HasPrefix(path, "docs/adr/") &&
		path != "docs/Architecture-Decision-Records.md" &&
		strings.EqualFold(filepath.Ext(path), ".md")
}

// contentsProblems reports once per page how its contents list disagrees with its headings.
func contentsProblems(sourcePath string, content []byte) []problem {
	if !needsContents(sourcePath) {
		return nil
	}
	headings := parseDocument(content, true).Headings
	title := firstTitle(headings)
	if title < 0 {
		if sections := sectionsOf(headings); len(sections) > 0 {
			return []problem{{Source: sourcePath, Line: sections[0].Line,
				Message: "no # title above the page's sections: " + contentsRule}}
		}
		return nil
	}
	sections := sectionsOf(headings[title+1:])
	if len(sections) == 0 {
		return nil
	}
	entries := openingList(splitLines(content), headings[title].Line)
	line, message := compareContents(entries, sections, headings[title].Line)
	if message == "" {
		return nil
	}
	return []problem{{Source: sourcePath, Line: line, Message: message}}
}

// firstTitle is the index of the page's # title, or -1 when it has none.
func firstTitle(headings []heading) int {
	for index, item := range headings {
		if item.Level == 1 {
			return index
		}
	}
	return -1
}

// sectionsOf keeps the headings a contents list names: levels two to four.
func sectionsOf(headings []heading) []heading {
	var sections []heading
	for _, item := range headings {
		if item.Level >= 2 && item.Level <= 4 {
			sections = append(sections, item)
		}
	}
	return sections
}

// openingList reads the list that starts on the first non-blank line after the
// title, and ends at the first line that is not one of its entries.
func openingList(lines [][]byte, titleLine int) []contentsEntry {
	index := titleLine
	for index < len(lines) && strings.TrimSpace(string(lines[index])) == "" {
		index++
	}
	var entries []contentsEntry
	for ; index < len(lines); index++ {
		match := contentsItemPattern.FindStringSubmatch(string(lines[index]))
		if match == nil {
			break
		}
		entries = append(entries, contentsEntry{
			Line:   index + 1,
			Indent: len(match[1]),
			Text:   match[2],
			Anchor: match[3],
		})
	}
	return entries
}

// compareContents returns the line and description of the first way the
// entries disagree with the sections, or "" when they agree.
func compareContents(entries []contentsEntry, sections []heading, titleLine int) (int, string) {
	if len(entries) == 0 {
		return titleLine, "no contents list: " + contentsRule
	}
	for index, section := range sections {
		if index >= len(entries) {
			return entries[len(entries)-1].Line, fmt.Sprintf(
				"contents list does not name %q (#%s): %s", section.Text, section.Anchor, contentsRule)
		}
		if message := entryIssue(entries[index], section); message != "" {
			return entries[index].Line, message
		}
	}
	if extra := entries[len(sections):]; len(extra) > 0 {
		return extra[0].Line, fmt.Sprintf(
			"contents entry links #%s, which is no further ## to #### heading on the page", extra[0].Anchor)
	}
	return 0, ""
}

// entryIssue describes how one entry fails to name its section, or "".
func entryIssue(entry contentsEntry, section heading) string {
	if entry.Anchor != section.Anchor {
		return fmt.Sprintf("contents entry links #%s where the next heading is %q (#%s)",
			entry.Anchor, section.Text, section.Anchor)
	}
	if want := 2 * (section.Level - 2); entry.Indent != want {
		return fmt.Sprintf("contents entry %q is indented %d spaces; a level-%d heading is indented %d",
			entry.Text, entry.Indent, section.Level, want)
	}
	if slugifyHeading(entry.Text) != slugifyHeading(section.Text) {
		return fmt.Sprintf("contents entry %q does not name its heading %q", entry.Text, section.Text)
	}
	return ""
}
