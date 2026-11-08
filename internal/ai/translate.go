package ai

import (
	"encoding/json"
	"fmt"
	"math"
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
	Flags    []string   `json:"-"`
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

// SystemPrompt returns the instructions for the model
func SystemPrompt(sourceLang, targetLang string) string {
	return fmt.Sprintf(`You are a professional software localizer translating user interface strings from %s to %s.
Rules:
1. Each item may contain "glossary": a list of required term translations. When a glossary term appears in the source you MUST use its given translation (you may adjust letter case or add affixes required by grammar). The glossary overrides your own preferences.
2. Keep every format placeholder exactly as in the source (e.g. %%s, %%d, %%1$s, {name}, $VAR), and keep markup tags and entities unchanged.
3. Keep leading and trailing whitespace and newlines (\n) as in the source.
4. If the source has a keyboard accelerator marker (_ or & before a letter), put exactly one marker before a suitable letter of the translation.
5. "context" and "notes" explain where the string is used; do not translate them.
6. When "forms" is given, return that many plural forms (form 0 first) following the target language's plural rules.
7. Use concise, natural, consistent UI language. Do not add explanations.
Reply with JSON only: {"translations":[{"id":<id>,"forms":["..."]}]} with one entry per input item.`, sourceLang, targetLang)
}

// UserPrompt serializes a batch of items
func UserPrompt(items []Item) string {
	b, _ := json.Marshal(struct {
		Items []Item `json:"items"`
	}{items})
	return string(b)
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
	ProblemFormat   = "format"
	ProblemEmpty    = "empty"
)

// Verify checks a model translation: format placeholders, newlines and use of
// the required glossary translations. A non-empty result means the translation
// must be reviewed by a person.
func Verify(it Item, forms []string, nplurals int) []string {
	var problems []string
	for _, f := range forms {
		if strings.TrimSpace(f) == "" {
			return []string{ProblemEmpty}
		}
	}
	pe := &po.Entry{ID: it.Source, IDPlural: it.Plural, Translation: forms, Flags: it.Flags}
	if len(po.Validate(pe, nplurals)) > 0 {
		problems = append(problems, ProblemFormat)
	}
	joined := strings.ToLower(glossary.Clean(strings.Join(forms, "\n")))
	joined = strings.NewReplacer("_", "", "&", "").Replace(joined)
	for _, h := range it.Glossary {
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
