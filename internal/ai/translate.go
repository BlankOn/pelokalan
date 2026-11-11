package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"pelokalan/internal/glossary"
	"pelokalan/internal/po"
)

// TermHint is a glossary term the translation must use
type TermHint struct {
	Term        string `json:"term"`
	Translation string `json:"translation"`
	// Optional: the file's Konteks note talks about a word of this term, so the note
	// decides and the glossary translation is only a suggestion
	Optional bool `json:"optional,omitempty"`
}

// quotedRe finds the parts of a note written in backticks or (straight or curly) double
// quotes: `key`, "flat review", “key”. Only those name terms the note decides on.
var quotedRe = regexp.MustCompile("`([^`\\n]{1,80})`|\"([^\"\\n]{1,80})\"|“([^”\\n]{1,80})”")

// ContextWords returns the words (lowercase) a Konteks note explicitly talks about: the words
// inside backticks or double quotes. Glossary terms containing one of them are decided by the
// note. Words merely used in sentences or examples do not count.
func ContextWords(note string) map[string]bool {
	var quoted []string
	for _, m := range quotedRe.FindAllStringSubmatch(note, -1) {
		quoted = append(quoted, m[1]+m[2]+m[3])
	}
	words := map[string]bool{}
	for _, w := range splitWords(strings.ToLower(strings.Join(quoted, " "))) {
		w = strings.Trim(w, "-'")
		if utf8.RuneCountInString(w) >= 3 {
			words[w] = true
		}
	}
	return words
}

// Mentioned reports whether the note's words include a word of text (3+ letters)
func Mentioned(text string, words map[string]bool) bool {
	if len(words) == 0 {
		return false
	}
	for _, w := range splitWords(glossary.Normalize(glossary.Clean(text))) {
		if utf8.RuneCountInString(w) >= 3 && words[strings.Trim(w, "-'")] {
			return true
		}
	}
	return false
}

// MatchCase adapts the capitalization of glossary translations to the source text. Glossary
// terms mostly come from interface labels ("Contrast" → "Kontras"); when the source uses the
// term in lowercase mid-sentence ("the mid-range contrast"), the hint becomes "kontras" so the
// model does not copy the label's capital letter. Acronyms and all-caps words are kept.
func MatchCase(hints []TermHint, source string) []TermHint {
	words := strings.Fields(glossary.Clean(source))
	for i, h := range hints {
		termWords := strings.Fields(h.Term)
		if len(termWords) == 0 {
			continue
		}
		first := strings.ToLower(strings.Trim(termWords[0], ".,;:!?()\"'"))
		usedLower, usedUpper := false, false
		for _, w := range words {
			bare := strings.Trim(w, ".,;:!?()\"'…<>")
			if strings.ToLower(bare) != first || bare == "" {
				continue
			}
			if r, _ := utf8.DecodeRuneInString(bare); unicode.IsUpper(r) {
				usedUpper = true
			} else {
				usedLower = true
			}
		}
		if usedLower && !usedUpper {
			hints[i].Translation = lowerFirst(h.Translation)
		}
	}
	return hints
}

// lowerFirst lowercases the first letter unless the first word is an acronym (all caps)
func lowerFirst(s string) string {
	first := strings.Fields(s)
	if len(first) == 0 {
		return s
	}
	if utf8.RuneCountInString(first[0]) > 1 && first[0] == strings.ToUpper(first[0]) {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[n:]
}

// MarkOptional makes glossary hints the note talks about optional
func MarkOptional(hints []TermHint, words map[string]bool) []TermHint {
	for i := range hints {
		if Mentioned(hints[i].Term, words) {
			hints[i].Optional = true
		}
	}
	return hints
}

// Item is one string to translate
type Item struct {
	ID       int64      `json:"id"`
	Source   string     `json:"source"`
	Plural   string     `json:"plural,omitempty"`
	Context  string     `json:"context,omitempty"`
	Notes    string     `json:"notes,omitempty"`
	Forms    int        `json:"forms,omitempty"` // number of plural forms wanted
	Glossary []TermHint `json:"glossary,omitempty"`
	// Current is the existing translation of an entry that needs review: a draft to improve
	Current []string `json:"current,omitempty"`
	// Alternative numbers the copies of one string when several alternative translations are
	// asked for (1 = the best one); 0 = a normal request
	Alternative int      `json:"alternative,omitempty"`
	Flags       []string `json:"-"`
}

// Term is a preferred glossary translation
type Term struct {
	Source      string // cleaned source, e.g. "Open Link"
	Translation string // most used translation
	Share       float64
	Words       int
}

// Glossary looks up preferred translations by normalized source text
type Glossary struct {
	terms    map[string]Term
	maxWords int
}

// NewGlossary indexes terms by their normalized source
func NewGlossary(terms []Term) *Glossary {
	g := &Glossary{terms: make(map[string]Term, len(terms))}
	for _, t := range terms {
		g.terms[glossary.Normalize(t.Source)] = t
		if t.Words > g.maxWords && t.Words <= glossary.TermMaxWords {
			g.maxWords = t.Words
		}
	}
	return g
}

// Len returns the number of terms
func (g *Glossary) Len() int {
	if g == nil {
		return 0
	}
	return len(g.terms)
}

// MinExactShare is how dominant a glossary translation must be to be applied
// without asking the model
const MinExactShare = 0.6

// Exact returns the glossary translation of a whole string when the string has
// no accelerator or trailing punctuation that would need to be reproduced.
func (g *Glossary) Exact(source string) (string, bool) {
	if g == nil || source != glossary.Clean(source) {
		return "", false
	}
	t, ok := g.terms[glossary.Normalize(source)]
	if !ok || t.Share < MinExactShare {
		return "", false
	}
	return t.Translation, true
}

// maxHints caps glossary hints per string
const maxHints = 12

// Matches returns glossary terms found in the source, longest phrases first;
// words covered by a longer phrase are not matched again.
func (g *Glossary) Matches(source string) []TermHint {
	terms := g.MatchTerms(source)
	out := make([]TermHint, len(terms))
	for i, t := range terms {
		out[i] = TermHint{Term: t.Source, Translation: t.Translation}
	}
	return out
}

// MatchTerms is Matches with the full term records (share, word count)
func (g *Glossary) MatchTerms(source string) []Term {
	if g.Len() == 0 {
		return nil
	}
	words := splitWords(glossary.Normalize(glossary.Clean(source)))
	covered := make([]bool, len(words))
	var out []Term
	seen := map[string]bool{}
	for n := min(g.maxWords, len(words)); n >= 1; n-- {
		for i := 0; i+n <= len(words); i++ {
			if anyCovered(covered[i : i+n]) {
				continue
			}
			key := strings.Join(words[i:i+n], " ")
			if n == 1 && utf8.RuneCountInString(key) < 3 {
				continue
			}
			t, ok := g.terms[key]
			if !ok || seen[key] {
				continue
			}
			seen[key] = true
			for j := i; j < i+n; j++ {
				covered[j] = true
			}
			out = append(out, t)
			if len(out) >= maxHints {
				return out
			}
		}
	}
	return out
}

func anyCovered(c []bool) bool {
	for _, v := range c {
		if v {
			return true
		}
	}
	return false
}

// splitWords splits text into words, keeping inner hyphens and apostrophes
// ("e-mail", "don't") and dropping format placeholders.
func splitWords(s string) []string {
	s = glossary.StripPlaceholders(s)
	return strings.FieldsFunc(s, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '\'')
	})
}

// MaxFileContext caps how much of the file's Konteks note goes into the prompt;
// MaxProjectContext is the longest project-wide guidance accepted in the AI settings
const (
	MaxFileContext    = 4000
	MaxProjectContext = 4000
)

// SystemPrompt returns the instructions for the model. fileContext is the translators'
// Konteks note about the PO file (may be empty); it is background only and never
// overrides the rules.
func SystemPrompt(sourceLang, targetLang, projectContext, fileContext string) string {
	p := fmt.Sprintf(`You are a professional software localizer translating user interface strings from %s to %s.
Rules:
1. Each item may contain "glossary": a list of term translations. When a glossary term appears in the source you MUST use its given translation (you may adjust letter case or add affixes required by grammar); it overrides your own preferences. A glossary entry fixes the word, not its capitalization: many entries come from interface labels written with a capital letter, so use normal sentence capitalization (lowercase in the middle of a sentence unless it is a name or the source capitalizes it). Exception: a term marked "optional": true is discussed in the project or file context below; there that context decides, and the glossary translation is only a suggestion.
2. Keep every format placeholder exactly as in the source (e.g. %%s, %%d, %%1$s, {name}, $VAR), and keep markup tags and entities unchanged.
3. Keep leading and trailing whitespace and newlines (\n) as in the source.
4. If the source has a keyboard accelerator marker (_ or & before a letter), put exactly one marker before a suitable letter of the translation.
5. "context" and "notes" explain where the string is used; do not translate them.
6. When "forms" is given, return that many plural forms (form 0 first) following the target language's plural rules.
6a. When "current" is given, it is the existing translation, marked for review, usually written by a person. Treat it as a draft to improve, not to replace: return it unchanged when its meaning is correct and placeholders are intact. Otherwise fix only what is wrong (wrong or missing meaning, broken placeholders, grammar errors, a term the file or project context asks for) and keep every other word. Never swap a correct word for a synonym, never rephrase for style, and treat the item's glossary terms as suggestions: keep the draft's own word choice when it is a correct translation.
7. Use concise, natural, consistent UI language. Do not add explanations.
Reply with JSON only: {"translations":[{"id":<id>,"forms":["..."]}]} with one entry per input item.`, sourceLang, targetLang)
	if projectContext = strings.TrimSpace(projectContext); projectContext != "" {
		if r := []rune(projectContext); len(r) > MaxProjectContext {
			projectContext = string(r[:MaxProjectContext]) + "…"
		}
		p += "\n\nGuidance for the whole project from its translators (style, audience, terminology). " +
			"Follow it for every item. When it says how a word should be translated, follow it " +
			"(it overrides glossary terms marked optional). Placeholders, markup and the other rules above still apply.\n" +
			"<project_context>\n" + projectContext + "\n</project_context>"
	}
	if fileContext = strings.TrimSpace(fileContext); fileContext != "" {
		if r := []rune(fileContext); len(r) > MaxFileContext {
			fileContext = string(r[:MaxFileContext]) + "…"
		}
		p += "\n\nContext about this file from its translators (what it is, where its strings appear, agreed terms and style). " +
			"Use it to choose meaning, terminology and tone; it is more specific than the project guidance and wins if they disagree. " +
			"When it says how a word should be translated in this file, follow it " +
			"(it overrides glossary terms marked optional). Placeholders, markup and the other rules above still apply.\n" +
			"<file_context>\n" + fileContext + "\n</file_context>"
	}
	return p
}

// UserPrompt serializes a batch of items. Markup is sent as-is: with the default HTML
// escaping, "<gui>" becomes "\u003cgui\u003e" and models echo those escapes back mangled
// (as control characters), which destroyed tags in translations.
func UserPrompt(items []Item) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(struct {
		Items []Item `json:"items"`
	}{items})
	return strings.TrimRight(b.String(), "\n")
}

// HasControlChars reports invisible C0 control characters (tab and newlines allowed)
func HasControlChars(forms []string) bool {
	for _, f := range forms {
		for _, r := range f {
			if (r < 0x20 && r != '\t' && r != '\n' && r != '\r') || r == 0x7f {
				return true
			}
		}
	}
	return false
}

// ParseResponse extracts translations per item id
func ParseResponse(content string) (map[int64][]string, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	var resp struct {
		Translations []struct {
			ID    int64    `json:"id"`
			Forms []string `json:"forms"`
			Text  string   `json:"text"`
		} `json:"translations"`
	}
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON from model: %w", err)
	}
	out := make(map[int64][]string, len(resp.Translations))
	for _, t := range resp.Translations {
		forms := t.Forms
		if len(forms) == 0 && t.Text != "" {
			forms = []string{t.Text}
		}
		if len(forms) > 0 {
			out[t.ID] = forms
		}
	}
	return out, nil
}

// Problem codes reported by Verify
const (
	ProblemGlossary = "glossary"
	ProblemCorrupt  = "corrupt" // control characters in the result: not saved
	ProblemFormat   = "format"
	ProblemEmpty    = "empty"
)

// Verify checks a model translation: format placeholders, newlines and use of
// the required glossary translations. A non-empty result means the translation
// must be reviewed by a person.
func Verify(it Item, forms []string, nplurals int, pluralForms string) []string {
	var problems []string
	for _, f := range forms {
		if strings.TrimSpace(f) == "" {
			return []string{ProblemEmpty}
		}
	}
	// garbage (e.g. markup turned into control characters) is never saved
	if HasControlChars(forms) {
		return []string{ProblemCorrupt}
	}
	pe := &po.Entry{ID: it.Source, IDPlural: it.Plural, Translation: forms, Flags: it.Flags}
	if len(po.ValidateWith(pe, po.PluralFor(pluralForms, nplurals))) > 0 {
		problems = append(problems, ProblemFormat)
	}
	joined := strings.ToLower(glossary.Clean(strings.Join(forms, "\n")))
	joined = strings.NewReplacer("_", "", "&", "").Replace(joined)
	for _, h := range it.Glossary {
		if h.Optional {
			continue // the file context may ask for another translation
		}
		if !strings.Contains(joined, strings.ToLower(h.Translation)) {
			problems = append(problems, ProblemGlossary)
			break
		}
	}
	return problems
}

// FitForms adjusts the number of plural forms to what the catalog needs
func FitForms(forms []string, plural bool, nplurals int) []string {
	if !plural {
		return forms[:1]
	}
	out := make([]string, nplurals)
	for i := range out {
		switch {
		case i < len(forms):
			out[i] = forms[i]
		case len(forms) > 0:
			out[i] = forms[len(forms)-1]
		}
	}
	return out
}

// EstimateTokens approximates the token count of a text (about 3.5 characters
// per token for Latin scripts, one per character for CJK).
func EstimateTokens(s string) int64 {
	var latin, other int
	for _, r := range s {
		if r < 0x2E80 {
			latin++
		} else {
			other++
		}
	}
	return int64(math.Ceil(float64(latin)/3.5)) + int64(other)
}

// Estimate is the predicted usage of translating items
type Estimate struct {
	PromptTokens     int64
	CompletionTokens int64
	Batches          int
}

// EstimateBatches predicts token usage for items sent in batches
func EstimateBatches(batches [][]Item, system string) Estimate {
	est := Estimate{Batches: len(batches)}
	sys := EstimateTokens(system) + 10
	for _, b := range batches {
		est.PromptTokens += sys + EstimateTokens(UserPrompt(b))
		for _, it := range b {
			forms := int64(max(1, it.Forms))
			src := EstimateTokens(it.Source)
			if it.Plural != "" {
				src = max(src, EstimateTokens(it.Plural))
			}
			// translations tend to be ~30% longer, plus JSON framing
			est.CompletionTokens += forms*(src*13/10+4) + 8
		}
		est.CompletionTokens += 10
	}
	return est
}

// Batch splits items into requests of at most maxItems items and roughly
// maxChars characters of source text.
func Batch(items []Item, maxItems, maxChars int) [][]Item {
	var out [][]Item
	var cur []Item
	size := 0
	for _, it := range items {
		n := len(it.Source) + len(it.Plural) + len(it.Notes)
		for _, c := range it.Current {
			n += len(c)
		}
		if len(cur) > 0 && (len(cur) >= maxItems || size+n > maxChars) {
			out = append(out, cur)
			cur, size = nil, 0
		}
		cur = append(cur, it)
		size += n
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// SortTerms orders terms for stable output (used by tests and listings)
func SortTerms(t []TermHint) {
	sort.Slice(t, func(i, j int) bool { return t[i].Term < t[j].Term })
}

// AlternativesNote is added to the system prompt when one string is sent several times to get
// alternative translations to choose from
func AlternativesNote(n int) string {
	return fmt.Sprintf(`

Alternatives: this request contains the same string %d times, numbered by "alternative" (1 to %d).
Give each a different translation a translator could pick from: vary word choice or sentence
structure, never just punctuation or capitalization. Every alternative must be correct and follow
the glossary hints, the context and the format rules. Alternative 1 is the one you consider best;
when it carries a "current" draft, the draft rules apply to alternative 1 only.`, n, n)
}
