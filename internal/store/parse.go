package store

import (
	"bufio"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// contextMarker appears in the info string of a context block's fence and
// tells it apart from a code sample written in the comment above it.
const contextMarker = "context"

var (
	fileHeaderRe  = regexp.MustCompile("^## `(.+)`$")
	lineHeaderRe  = regexp.MustCompile(`^#### Line (\d+)(.*)$`)
	contextLineRe = regexp.MustCompile(`^(\d+):(?: (.*))?$`)
	startedRe     = regexp.MustCompile(`^_Started: (.+)_$`)
	baseRe        = regexp.MustCompile(`^_Base: (.+)_$`)
	createdRe     = regexp.MustCompile(`^_Created: (.+)_$`)
	modifiedRe    = regexp.MustCompile(`^_Modified: (.+)_$`)
	fenceRe       = regexp.MustCompile("^(`{3,}|~{3,})")
)

// reviewMeta is what a REVIEW.md records about the review itself: where it was
// taken from and when it was begun and last written. All fields are empty for
// a file that records none of them.
type reviewMeta struct {
	data     map[string]map[int]*Annotation
	started  string
	base     string
	created  string
	modified string
}

// parse reads a REVIEW.md file and returns the annotations together with the
// review's own record, which is empty for a file that carries none.
//
// Comments are kept as the reviewer wrote them, so the document's own
// structure is only recognised outside fenced code blocks: a heading in a
// comment's code sample is part of the comment. A horizontal rule separates
// sections only where a file heading follows it.
func parse(path string) (reviewMeta, error) {
	lines, err := readLines(path)
	if err != nil {
		if os.IsNotExist(err) {
			return reviewMeta{data: make(map[string]map[int]*Annotation)}, nil
		}
		return reviewMeta{}, err
	}

	data := make(map[string]map[int]*Annotation)

	// A document that marks its context blocks is read strictly, so a code
	// sample closing a comment is never taken for one. Files written before
	// the marker carry none; there the numbered source lines identify it.
	marked := usesContextMarker(lines)

	var (
		meta        reviewMeta
		currentFile string
		currentLine int
		outdated    bool
		body        []string
		collecting  bool
		fence       string
	)

	// save turns the lines collected since the last heading into an annotation.
	save := func() {
		if !collecting {
			return
		}
		comment, context, contextFrom := splitBody(body, marked)
		if comment != "" && currentFile != "" {
			data[currentFile][currentLine] = &Annotation{
				Comment:     comment,
				Context:     context,
				ContextFrom: contextFrom,
				Outdated:    outdated,
			}
		}
		body = nil
		collecting = false
	}

	for i, line := range lines {
		// Nothing inside a code fence is structure
		if fence != "" {
			if closesFence(line, fence) {
				fence = ""
			}
			if collecting {
				body = append(body, line)
			}
			continue
		}
		if marker := fenceMarker(line); marker != "" {
			fence = marker
			if collecting {
				body = append(body, line)
			}
			continue
		}

		if m := lineHeaderRe.FindStringSubmatch(line); m != nil && currentFile != "" {
			save()
			currentLine, _ = strconv.Atoi(m[1])
			outdated = strings.Contains(m[2], "outdated")
			collecting = true
			continue
		}
		if m := fileHeaderRe.FindStringSubmatch(line); m != nil {
			save()
			currentFile = m[1]
			if data[currentFile] == nil {
				data[currentFile] = make(map[int]*Annotation)
			}
			continue
		}
		if strings.TrimSpace(line) == "---" && opensFileSection(lines[i+1:]) {
			save()
			continue
		}

		if !collecting {
			readMeta(&meta, strings.TrimSpace(line))
			continue
		}
		body = append(body, unescapeStructure(line))
	}
	save()

	meta.data = data
	return meta, nil
}

// readMeta records a metadata line, of the form _Name: value_. Each field is
// kept at the first value seen, so the document's header wins over anything
// similar that follows.
func readMeta(meta *reviewMeta, line string) {
	for _, f := range []struct {
		re   *regexp.Regexp
		into *string
	}{
		{startedRe, &meta.started},
		{baseRe, &meta.base},
		{createdRe, &meta.created},
		{modifiedRe, &meta.modified},
	} {
		if *f.into != "" {
			continue
		}
		if m := f.re.FindStringSubmatch(line); m != nil {
			*f.into = m[1]
		}
	}
}

// splitBody separates an annotation's comment from the context block closing
// it. With marked set, only a block whose fence carries the context marker
// counts as one; otherwise a closing block of numbered source lines does.
func splitBody(body []string, marked bool) (string, []string, int) {
	end := len(body)
	for end > 0 && strings.TrimSpace(body[end-1]) == "" {
		end--
	}

	open, close, ok := lastFencedBlock(body[:end])
	if !ok || close != end-1 {
		return joinComment(body[:end]), nil, 0
	}
	if marked && !isContextFence(body[open]) {
		return joinComment(body[:end]), nil, 0
	}
	if context, from, ok := parseContext(body[open+1 : close]); ok {
		return joinComment(body[:open]), context, from
	}
	return joinComment(body[:end]), nil, 0
}

// usesContextMarker reports whether the document marks its context blocks.
func usesContextMarker(lines []string) bool {
	for _, line := range lines {
		if fenceMarker(line) != "" && isContextFence(line) {
			return true
		}
	}
	return false
}

// isContextFence reports whether a fence line opens a context block.
func isContextFence(line string) bool {
	trimmed, _ := trimIndent(line)
	info := strings.TrimLeft(trimmed, "`~")
	return slices.Contains(strings.Fields(info), contextMarker)
}

// joinComment assembles the comment body, without the blank lines that set it
// apart from the headings around it.
func joinComment(lines []string) string {
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// parseContext reads numbered source lines, the form the context block takes,
// and reports whether the lines are such a block at all.
func parseContext(lines []string) ([]string, int, bool) {
	if len(lines) == 0 {
		return nil, 0, false
	}

	context := make([]string, 0, len(lines))
	from := 0
	for _, line := range lines {
		m := contextLineRe.FindStringSubmatch(line)
		if m == nil {
			return nil, 0, false
		}
		if from == 0 {
			from, _ = strconv.Atoi(m[1])
		}
		context = append(context, m[2])
	}
	return context, from, true
}

// lastFencedBlock returns the lines opening and closing the last complete
// fenced block in lines.
func lastFencedBlock(lines []string) (open, close int, ok bool) {
	fence := ""
	start := 0
	for i, line := range lines {
		if fence == "" {
			if marker := fenceMarker(line); marker != "" {
				fence, start = marker, i
			}
			continue
		}
		if closesFence(line, fence) {
			open, close, ok = start, i, true
			fence = ""
		}
	}
	return open, close, ok
}

// opensFileSection reports whether the next line with content starts a file
// section, which is what makes a horizontal rule a separator rather than text.
func opensFileSection(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return fileHeaderRe.MatchString(line)
	}
	return false
}

// fenceMarker returns the run of backticks or tildes opening a code fence on
// this line, or an empty string when the line opens none.
func fenceMarker(line string) string {
	trimmed, indent := trimIndent(line)
	if indent > maxFenceIndent {
		return ""
	}
	return fenceRe.FindString(trimmed)
}

// closesFence reports whether the line closes a fence opened with marker.
func closesFence(line, marker string) bool {
	trimmed, indent := trimIndent(line)
	if indent > maxFenceIndent {
		return false
	}
	trimmed = strings.TrimRight(trimmed, " ")
	if len(trimmed) < len(marker) {
		return false
	}
	return strings.Trim(trimmed, marker[:1]) == ""
}

// maxFenceIndent is how far a code fence may be indented before it is content
// of an enclosing block rather than a fence of its own.
const maxFenceIndent = 3

// trimIndent strips leading spaces and reports how many there were.
func trimIndent(line string) (string, int) {
	trimmed := strings.TrimLeft(line, " ")
	return trimmed, len(line) - len(trimmed)
}

// looksStructural reports whether a line would be read as one of the
// document's own headings rather than as text.
func looksStructural(line string) bool {
	return fileHeaderRe.MatchString(line) || lineHeaderRe.MatchString(line)
}

// unescapeStructure removes the backslash that escapeComment puts in front of
// a comment line which would otherwise read as a heading.
func unescapeStructure(line string) string {
	if rest, found := strings.CutPrefix(line, `\`); found && looksStructural(rest) {
		return rest
	}
	return line
}

// readLines reads a file into its lines, without the line terminators.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineLength)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
