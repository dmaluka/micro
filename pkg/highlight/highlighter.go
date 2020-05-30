package highlight

import (
	"fmt"
	"regexp"
	"strings"
)

func sliceStart(slc []byte, index int) []byte {
	len := len(slc)
	i := 0
	totalSize := 0
	for totalSize < len {
		if i >= index {
			return slc[totalSize:]
		}

		_, _, size := DecodeCharacter(slc[totalSize:])
		totalSize += size
		i++
	}

	return slc[totalSize:]
}

func sliceEnd(slc []byte, index int) []byte {
	len := len(slc)
	i := 0
	totalSize := 0
	for totalSize < len {
		if i >= index {
			return slc[:totalSize]
		}

		_, _, size := DecodeCharacter(slc[totalSize:])
		totalSize += size
		i++
	}

	return slc[:totalSize]
}

// RunePos returns the rune index of a given byte index
// This could cause problems if the byte index is between code points
func runePos(p int, str []byte) int {
	if p < 0 {
		return 0
	}
	if p >= len(str) {
		return CharacterCount(str)
	}
	return CharacterCount(str[:p])
}

func combineLineMatch(src, dst LineMatch) LineMatch {
	for k, v := range src {
		if g, ok := dst[k]; ok {
			if g == 0 {
				dst[k] = v
			}
		} else {
			dst[k] = v
		}
	}
	return dst
}

// A State represents the region at the end of a line
type State *region

var EmptyDef = Def{nil, &rules{}}

// LineStates is an interface for a buffer-like object which can also store the states and matches for every line
type LineStates interface {
	LineBytes(n int) []byte
	LinesNum() int
	State(lineN int) State
	SetState(lineN int, s State)
	SetMatch(lineN int, m LineMatch)
}

// A Highlighter contains the information needed to highlight a string
type Highlighter struct {
	lastRegion *region
	Def        *Def

	customPatterns []*pattern
}

// NewHighlighter returns a new highlighter from the given syntax definition
func NewHighlighter(def *Def) *Highlighter {
	h := new(Highlighter)
	h.Def = def
	return h
}

// LineMatch represents the syntax highlighting matches for one line. Each index where the coloring is changed is marked with that
// color's group (represented as one byte)
type LineMatch map[int]Group

func findIndex(regex *regexp.Regexp, skip *regexp.Regexp, str []byte, canMatchStart, canMatchEnd bool) []int {
	regexStr := regex.String()
	if strings.Contains(regexStr, "^") {
		if !canMatchStart {
			return nil
		}
	}
	if strings.Contains(regexStr, "$") {
		if !canMatchEnd {
			return nil
		}
	}

	var strbytes []byte
	if skip != nil {
		strbytes = skip.ReplaceAllFunc(str, func(match []byte) []byte {
			res := make([]byte, CharacterCount(match))
			return res
		})
	} else {
		strbytes = str
	}

	match := regex.FindIndex(strbytes)
	if match == nil {
		return nil
	}
	// return []int{match.Index, match.Index + match.Length}
	return []int{runePos(match[0], str), runePos(match[1], str)}
}

func findAllIndex(regex *regexp.Regexp, str []byte, canMatchStart, canMatchEnd bool) [][]int {
	regexStr := regex.String()
	if strings.Contains(regexStr, "^") {
		if !canMatchStart {
			return nil
		}
	}
	if strings.Contains(regexStr, "$") {
		if !canMatchEnd {
			return nil
		}
	}
	matches := regex.FindAllIndex(str, -1)
	for i, m := range matches {
		matches[i][0] = runePos(m[0], str)
		matches[i][1] = runePos(m[1], str)
	}
	return matches
}

func (h *Highlighter) highlightRegion(highlights LineMatch, start int, canMatchEnd bool, lineNum int, line []byte, curRegion *region, statesOnly bool) LineMatch {
	lineLen := CharacterCount(line)
	if start == 0 {
		if !statesOnly {
			if _, ok := highlights[0]; !ok {
				highlights[0] = curRegion.group
			}
		}
	}

	loc := findIndex(curRegion.end, curRegion.skip, line, start == 0, canMatchEnd)
	if loc != nil {
		if !statesOnly {
			highlights[start+loc[0]] = curRegion.limitGroup
		}
		if curRegion.parent == nil {
			if !statesOnly {
				highlights[start+loc[1]] = 0
				h.highlightRegion(highlights, start, false, lineNum, sliceEnd(line, loc[0]), curRegion, statesOnly)
			}
			h.highlightEmptyRegion(highlights, start+loc[1], canMatchEnd, lineNum, sliceStart(line, loc[1]), statesOnly)
			return highlights
		}
		if !statesOnly {
			highlights[start+loc[1]] = curRegion.parent.group
			h.highlightRegion(highlights, start, false, lineNum, sliceEnd(line, loc[0]), curRegion, statesOnly)
		}
		h.highlightRegion(highlights, start+loc[1], canMatchEnd, lineNum, sliceStart(line, loc[1]), curRegion.parent, statesOnly)
		return highlights
	}

	if lineLen == 0 {
		if canMatchEnd {
			h.lastRegion = curRegion
		}

		return highlights
	}

	firstLoc := []int{lineLen, 0}

	var firstRegion *region
	for _, r := range curRegion.rules.regions {
		loc := findIndex(r.start, nil, line, start == 0, canMatchEnd)
		if loc != nil {
			if loc[0] < firstLoc[0] {
				firstLoc = loc
				firstRegion = r
			}
		}
	}
	if firstLoc[0] != lineLen {
		if !statesOnly {
			highlights[start+firstLoc[0]] = firstRegion.limitGroup
		}
		h.highlightRegion(highlights, start, false, lineNum, sliceEnd(line, firstLoc[0]), curRegion, statesOnly)
		h.highlightRegion(highlights, start+firstLoc[1], canMatchEnd, lineNum, sliceStart(line, firstLoc[1]), firstRegion, statesOnly)
		return highlights
	}

	if !statesOnly {
		fullHighlights := make([]Group, lineLen)
		for i := 0; i < len(fullHighlights); i++ {
			fullHighlights[i] = curRegion.group
		}

		for _, p := range curRegion.rules.patterns {
			matches := findAllIndex(p.regex, line, start == 0, canMatchEnd)
			for _, m := range matches {
				for i := m[0]; i < m[1]; i++ {
					fullHighlights[i] = p.group
				}
			}
		}
		for i, h := range fullHighlights {
			if i == 0 || h != fullHighlights[i-1] {
				highlights[start+i] = h
			}
		}
	}

	if canMatchEnd {
		h.lastRegion = curRegion
	}

	return highlights
}

func (h *Highlighter) highlightEmptyRegion(highlights LineMatch, start int, canMatchEnd bool, lineNum int, line []byte, statesOnly bool) LineMatch {
	lineLen := CharacterCount(line)
	if lineLen == 0 {
		if canMatchEnd {
			h.lastRegion = nil
		}
		return highlights
	}

	firstLoc := []int{lineLen, 0}
	var firstRegion *region
	for _, r := range h.Def.rules.regions {
		loc := findIndex(r.start, nil, line, start == 0, canMatchEnd)
		if loc != nil {
			if loc[0] < firstLoc[0] {
				firstLoc = loc
				firstRegion = r
			}
		}
	}
	if firstLoc[0] != lineLen {
		if !statesOnly {
			highlights[start+firstLoc[0]] = firstRegion.limitGroup
		}
		h.highlightEmptyRegion(highlights, start, false, lineNum, sliceEnd(line, firstLoc[0]), statesOnly)
		h.highlightRegion(highlights, start+firstLoc[1], canMatchEnd, lineNum, sliceStart(line, firstLoc[1]), firstRegion, statesOnly)
		return highlights
	}

	if statesOnly {
		if canMatchEnd {
			h.lastRegion = nil
		}

		return highlights
	}

	fullHighlights := make([]Group, len(line))
	for _, p := range h.Def.rules.patterns {
		matches := findAllIndex(p.regex, line, start == 0, canMatchEnd)
		for _, m := range matches {
			for i := m[0]; i < m[1]; i++ {
				fullHighlights[i] = p.group
			}
		}
	}
	for i, h := range fullHighlights {
		if i == 0 || h != fullHighlights[i-1] {
			// if _, ok := highlights[start+i]; !ok {
			highlights[start+i] = h
			// }
		}
	}

	if canMatchEnd {
		h.lastRegion = nil
	}

	return highlights
}

func (h *Highlighter) highlightCustomMatches(highlights LineMatch, line []byte) LineMatch {
	lineLen := CharacterCount(line)

	for _, p := range h.customPatterns {
		matches := findAllIndex(p.regex, line, true, true)
		for _, m := range matches {
			var lastHighlight Group
			lastIdx := -1
			for i, h := range highlights {
				if i < m[1] && i > lastIdx {
					lastIdx = i
					lastHighlight = h
				}
				if i >= m[0] && i < m[1] {
					delete(highlights, i)
				}
			}

			highlights[m[0]] = p.group

			if lastIdx >= 0 && m[1] < lineLen {
				if _, ok := highlights[m[1]]; !ok {
					highlights[m[1]] = lastHighlight
				}
			}
		}
	}

	return highlights
}

// HighlightString syntax highlights a string
// Use this function for simple syntax highlighting and use the other functions for
// more advanced syntax highlighting. They are optimized for quick rehighlighting of the same
// text with minor changes made
func (h *Highlighter) HighlightString(input string) []LineMatch {
	lines := strings.Split(input, "\n")
	var lineMatches []LineMatch

	for i := 0; i < len(lines); i++ {
		line := []byte(lines[i])
		highlights := make(LineMatch)

		if i == 0 || h.lastRegion == nil {
			highlights = h.highlightEmptyRegion(highlights, 0, true, i, line, false)
		} else {
			highlights = h.highlightRegion(highlights, 0, true, i, line, h.lastRegion, false)
		}

		highlights = h.highlightCustomMatches(highlights, line)

		lineMatches = append(lineMatches, highlights)
	}

	return lineMatches
}

// HighlightStates correctly sets all states for the buffer
func (h *Highlighter) HighlightStates(input LineStates) {
	for i := 0; i < input.LinesNum(); i++ {
		line := input.LineBytes(i)
		// highlights := make(LineMatch)

		if i == 0 || h.lastRegion == nil {
			h.highlightEmptyRegion(nil, 0, true, i, line, true)
		} else {
			h.highlightRegion(nil, 0, true, i, line, h.lastRegion, true)
		}

		curState := h.lastRegion

		input.SetState(i, curState)
	}
}

// HighlightMatches sets the matches for each line from startline to endline
// It sets all other matches in the buffer to nil to conserve memory
// This assumes that all the states are set correctly
func (h *Highlighter) HighlightMatches(input LineStates, startline, endline int) {
	for i := startline; i <= endline; i++ {
		if i >= input.LinesNum() {
			break
		}

		line := input.LineBytes(i)
		highlights := make(LineMatch)

		var match LineMatch
		if i == 0 || input.State(i-1) == nil {
			match = h.highlightEmptyRegion(highlights, 0, true, i, line, false)
		} else {
			match = h.highlightRegion(highlights, 0, true, i, line, input.State(i-1), false)
		}

		match = h.highlightCustomMatches(match, line)

		input.SetMatch(i, match)
	}
}

// ReHighlightStates will scan down from `startline` and set the appropriate end of line state
// for each line until it comes across a line whose state does not change
// returns the number of the final line
func (h *Highlighter) ReHighlightStates(input LineStates, startline int) int {
	// lines := input.LineData()

	h.lastRegion = nil
	if startline > 0 {
		h.lastRegion = input.State(startline - 1)
	}
	for i := startline; i < input.LinesNum(); i++ {
		line := input.LineBytes(i)
		// highlights := make(LineMatch)

		// var match LineMatch
		if i == 0 || h.lastRegion == nil {
			h.highlightEmptyRegion(nil, 0, true, i, line, true)
		} else {
			h.highlightRegion(nil, 0, true, i, line, h.lastRegion, true)
		}
		curState := h.lastRegion
		lastState := input.State(i)

		input.SetState(i, curState)

		if curState == lastState {
			return i
		}
	}

	return input.LinesNum() - 1
}

// ReHighlightLine will rehighlight the state and match for a single line
func (h *Highlighter) ReHighlightLine(input LineStates, lineN int) {
	line := input.LineBytes(lineN)
	highlights := make(LineMatch)

	h.lastRegion = nil
	if lineN > 0 {
		h.lastRegion = input.State(lineN - 1)
	}

	var match LineMatch
	if lineN == 0 || h.lastRegion == nil {
		match = h.highlightEmptyRegion(highlights, 0, true, lineN, line, false)
	} else {
		match = h.highlightRegion(highlights, 0, true, lineN, line, h.lastRegion, false)
	}
	curState := h.lastRegion

	match = h.highlightCustomMatches(match, line)

	input.SetMatch(lineN, match)
	input.SetState(lineN, curState)
}

// AddCustomPattern adds a custom regex pattern for the given group.
// Custom patterns are highlighted independently of syntax highlighting. When displayed,
// they are overlayed on top of syntax highlighting. Each next custom pattern is overlayed
// on top of previous custom patterns.
func (h *Highlighter) AddCustomPattern(group, regex string) error {
	r, err := regexp.Compile(regex)
	if err != nil {
		return err
	}

	if _, ok := Groups[group]; !ok {
		numGroups++
		Groups[group] = numGroups
	}
	groupNum := Groups[group]

	for _, p := range h.customPatterns {
		if p.group == groupNum {
			return fmt.Errorf("Multiple custom patterns for the same group (%s) not supported", group)
		}
	}
	h.customPatterns = append(h.customPatterns, &pattern{groupNum, r})

	return nil
}

// RemoveCustomPattern removes custom pattern for the given group.
func (h *Highlighter) RemoveCustomPattern(group string) {
	for i, p := range h.customPatterns {
		if p.group.String() == group {
			h.customPatterns = append(h.customPatterns[:i], h.customPatterns[i+1:]...)
			break
		}
	}
}
