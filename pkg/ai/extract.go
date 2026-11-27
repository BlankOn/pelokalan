package ai

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"pelokalan/pkg/glossary"
)

// Glossary extraction: short strings ("Open File") become glossary terms as they are, but most of the vocabulary of
// documentation lives in long strings. A model reads a long string with its human translation and names the terms
// it contains and the words the translator used for them; code then keeps only pairs whose two sides really appear
// in the two texts, so nothing invented reaches the glossary.

// ExtractMinWords is the shortest source string (in words) sent for extraction; shorter ones are terms themselves
const ExtractMinWords = 4

// ExtractMaxChars skips strings longer than this (code listings, huge paragraphs)
const ExtractMaxChars = 3000

// ExtractMaxTerms caps the pairs kept per string
const ExtractMaxTerms = 15

// extractTermWords / extractTranslationWords limit the length of a pair's two sides
const (
	extractTermWords        = 4
	extractTranslationWords = 6
)

// ExtractItem is one translated string sent for extraction
type ExtractItem struct {
	ID          int64  `json:"id"`
	Source      string `json:"source"`
	Translation string `json:"translation"`
}

// Pair is a glossary term found in a string: the source words and the words the translator used for them
type Pair struct {
	Source      string `json:"s"`
	Translation string `json:"t"`
}

// ExtractSystemPrompt is the instruction for glossary extraction
func ExtractSystemPrompt(sourceLang, targetLang string) string {
	return fmt.Sprintf(`You build a bilingual glossary for software and documentation translators.
Each item is a %[1]s source string and its human translation into %[2]s. Name the glossary terms the source contains
and, for each, the exact words the translator used for it in this translation.

What to take:
- Content words and terms: nouns, verbs, adjectives, adverbs and technical terms ("module", "removed", "deprecated").
- Multi-word terms that belong together ("standard library", "keyword argument"); when their words are also
  translated one by one, give the parts too ("standard", "library").
- Words with a fixed rendering that a glossary helps keep consistent, such as "after", "before", "instead".

What to skip:
- Articles, pronouns, auxiliary and linking verbs, and the most common prepositions and conjunctions
  (the, a, an, this, that, it, is, are, be, was, has, of, to, in, on, for, and, or, but, with, by, as).
- Numbers, versions, names of people, products and projects ("Python", "GNOME"), code, identifiers, file names,
  URLs, placeholders (%%s, {name}, $1) and markup: text inside backticks, reStructuredText roles (:ref:`+"`...`"+`,
  :pep:`+"`...`"+`), XML/HTML tags.
- Any term for which the translation has no clear equivalent (left out, paraphrased, merged with other words).

Rules for every pair:
1. "s" is copied character for character from the source string, "t" character for character from the
   translation. Keep the form that appears there (e.g. "removed" -> "dihapus", "Removal" -> "Penghapusan"):
   never change it to a dictionary form, never fix spelling, never translate anything yourself.
2. "t" holds only the words that render "s", nothing around them. A single source word may map to a short
   phrase when the translator needed one ("deprecated" -> "dinyatakan usang").
3. "s" has at most %[3]d words, "t" at most %[4]d. One pair per term per item, at most %[5]d pairs per item,
   most useful first. An item without such terms gets an empty list.

Answer with JSON only, no explanation, in this shape:
{"items":[{"id":1,"terms":[{"s":"module","t":"modul"},{"s":"removed","t":"dihapus"}]}]}`,
		sourceLang, targetLang, extractTermWords, extractTranslationWords, ExtractMaxTerms)
}

// ExtractUserPrompt lists the items as JSON
func ExtractUserPrompt(items []ExtractItem) string {
	data, _ := json.Marshal(map[string]any{"items": items})
	return string(data)
}

// ParseExtractResponse reads the model's answer: item id -> pairs (unchecked)
func ParseExtractResponse(content string) (map[int64][]Pair, error) {
	content = strings.TrimSpace(content)
	if i := strings.Index(content, "{"); i > 0 { // a code fence or a stray word before the JSON
		content = content[i:]
	}
	if i := strings.LastIndex(content, "}"); i >= 0 && i < len(content)-1 {
		content = content[:i+1]
	}
	var resp struct {
		Items []struct {
			ID    int64             `json:"id"`
			Terms []json.RawMessage `json:"terms"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("unreadable model answer: %w", err)
	}
	out := make(map[int64][]Pair, len(resp.Items))
	for _, it := range resp.Items {
		pairs := []Pair{}
		for _, raw := range it.Terms {
			var p Pair
			if json.Unmarshal(raw, &p) != nil { // also accept ["source", "translation"]
				var two []string
				if json.Unmarshal(raw, &two) != nil || len(two) != 2 {
					continue
				}
				p = Pair{Source: two[0], Translation: two[1]}
			}
			pairs = append(pairs, p)
		}
		out[it.ID] = pairs
	}
	return out, nil
}

// roleRe finds backtick spans, with a reStructuredText role before them (:ref:`...`), and markupRe tags, entities and
// addresses: text that is not vocabulary, except the visible text of links and labels
var (
	roleRe   = regexp.MustCompile("(:[a-zA-Z][\\w.+-]*:)?(``?)([^`]*)`+")
	markupRe = regexp.MustCompile(`<[^<>]*>|&[a-zA-Z#0-9]+;|https?://\S+`)
)

// keepRoles are roles whose text is ordinary words (a glossary term, a button label)
var keepRoles = map[string]bool{":term:": true, ":dfn:": true, ":guilabel:": true, ":menuselection:": true, ":abbr:": true}

// ExtractText is a string as checked for pairs: code, markup and placeholders replaced by spaces. Links keep their
// visible text (":ref:`removed in 3.13 <whatsnew>`" -> "removed in 3.13").
func ExtractText(s string) string {
	s = roleRe.ReplaceAllStringFunc(s, func(m string) string {
		g := roleRe.FindStringSubmatch(m)
		role, ticks, text := g[1], g[2], g[3]
		if ticks == "``" { // inline code
			return " "
		}
		if i := strings.LastIndex(text, "<"); i > 0 && strings.HasSuffix(strings.TrimSpace(text), ">") {
			return " " + text[:i] + " " // a link with its own text
		}
		if keepRoles[role] {
			return " " + text + " "
		}
		return " "
	})
	return glossary.StripPlaceholders(markupRe.ReplaceAllString(s, " "))
}

// keptAsIs: a pair copied unchanged is no glossary pair when it looks like code or a name ("os.path", "Python")
func keptAsIs(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
		break
	}
	return strings.ContainsAny(s, "._()/\\=[]{}")
}

// stopWords are never a glossary term on their own (English source)
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "this": true, "that": true, "these": true, "those": true, "it": true,
	"its": true, "is": true, "are": true, "was": true, "were": true, "be": true, "been": true, "being": true,
	"has": true, "have": true, "had": true, "do": true, "does": true, "did": true, "of": true, "to": true,
	"in": true, "on": true, "at": true, "for": true, "and": true, "or": true, "but": true, "with": true,
	"by": true, "as": true, "if": true, "so": true, "not": true, "no": true, "you": true, "your": true,
	"we": true, "they": true, "he": true, "she": true, "i": true, "can": true, "will": true, "may": true,
	"from": true, "which": true, "who": true, "what": true, "there": true, "here": true, "also": true,
}

// CheckPairs keeps the pairs that really occur in the item: each side appears as whole words in its text outside
// markup (case is ignored), sides are short, not a stop word, not a number. Pairs are tidied (accelerators, trailing
// punctuation), duplicates dropped and at most ExtractMaxTerms kept.
func CheckPairs(it ExtractItem, pairs []Pair) []Pair {
	src, tr := strings.ToLower(ExtractText(it.Source)), strings.ToLower(ExtractText(it.Translation))
	var out []Pair
	seen := map[string]bool{}
	for _, p := range pairs {
		s, t := tidyPart(p.Source), tidyPart(p.Translation)
		if s == "" || t == "" || !hasLetters(s) || !hasLetters(t) {
			continue
		}
		sw, tw := len(strings.Fields(s)), len(strings.Fields(t))
		if sw > extractTermWords || tw > extractTranslationWords {
			continue
		}
		ls, lt := strings.ToLower(s), strings.ToLower(t)
		if sw == 1 && stopWords[ls] {
			continue
		}
		if ls == lt && keptAsIs(s) {
			continue
		}
		if !containsWords(src, ls) || !containsWords(tr, lt) {
			continue
		}
		key := ls + "\x00" + lt
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Pair{Source: s, Translation: t})
		if len(out) == ExtractMaxTerms {
			break
		}
	}
	return out
}

// tidyPart trims a pair side: spaces, quotes and punctuation at the ends, inner runs of spaces
func tidyPart(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || (unicode.IsPunct(r) && r != '-' && r != '_') || r == '`'
	})
}

func hasLetters(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// containsWords reports whether needle occurs in hay with word boundaries on both sides (both lowercase)
func containsWords(hay, needle string) bool {
	for off := 0; off < len(hay); {
		i := strings.Index(hay[off:], needle)
		if i < 0 {
			return false
		}
		at := off + i
		end := at + len(needle)
		if boundaryBefore(hay, at) && boundaryAfter(hay, end) {
			return true
		}
		off = at + 1
	}
	return false
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func boundaryBefore(s string, i int) bool {
	if i == 0 {
		return true
	}
	r := []rune(s[:i])
	return !isWordRune(r[len(r)-1])
}

func boundaryAfter(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	for _, r := range s[i:] {
		return !isWordRune(r)
	}
	return true
}

// ExtractCandidate reports whether a translated string is long enough to be worth sending for extraction
func ExtractCandidate(source, translation string) bool {
	if len(source) > ExtractMaxChars || strings.TrimSpace(translation) == "" {
		return false
	}
	n := 0
	for _, w := range strings.Fields(ExtractText(source)) {
		if hasLetters(w) {
			n++
		}
	}
	return n >= ExtractMinWords
}

// ExtractBatch splits items into batches of at most maxItems items and about maxChars characters
func ExtractBatch(items []ExtractItem, maxItems, maxChars int) [][]ExtractItem {
	var out [][]ExtractItem
	var cur []ExtractItem
	size := 0
	for _, it := range items {
		n := len(it.Source) + len(it.Translation)
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

// ExtractCompletionTokens is the expected answer size per item, for estimates
const ExtractCompletionTokens = 90
