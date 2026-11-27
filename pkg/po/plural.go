package po

import (
	"strconv"
	"strings"
	"sync"
)

// Plural describes a language's plural forms the way msgfmt uses them for its format
// checks: how many forms there are and which ones are used for at most one number
// (e.g. form 0 = "n is 1" in English). Only such a form may leave out the number
// placeholder; every other form must have the same placeholders as msgid_plural.
type Plural struct {
	N      int
	single []bool // single[i]: form i is used for at most one n in 0..1000
}

// SingleValue reports whether plural form i is used for at most one number
func (p Plural) SingleValue(i int) bool { return i >= 0 && i < len(p.single) && p.single[i] }

var pluralCache sync.Map // "<nplurals>|<plural forms>" -> Plural

// PluralFor parses a Plural-Forms value ("nplurals=2; plural=(n != 1);"). Without a usable
// formula it falls back to gettext's default (one form for n == 1, one for the rest)
// when there are two forms, and to "no single-value form" otherwise.
func PluralFor(pluralForms string, nplurals int) Plural {
	key := strconv.Itoa(nplurals) + "|" + pluralForms
	if v, ok := pluralCache.Load(key); ok {
		return v.(Plural)
	}
	p := computePlural(pluralForms, nplurals)
	pluralCache.Store(key, p)
	return p
}

func computePlural(pluralForms string, nplurals int) Plural {
	if strings.Contains(pluralForms, "nplurals") {
		nplurals = NPlurals(pluralForms)
	}
	if nplurals < 1 {
		nplurals = 1
	}
	p := Plural{N: nplurals, single: make([]bool, nplurals)}
	expr := ""
	if i := strings.Index(pluralForms, "plural="); i >= 0 {
		expr = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(pluralForms[i+len("plural="):]), ";"))
	}
	node, ok := parsePluralExpr(expr)
	if !ok {
		if nplurals == 2 {
			node, _ = parsePluralExpr("n != 1")
		} else {
			return p
		}
	}
	counts := make([]int, nplurals)
	for n := 0; n <= 1000; n++ {
		if i := node.eval(n); i >= 0 && i < nplurals {
			counts[i]++
		}
	}
	for i, c := range counts {
		p.single[i] = c <= 1
	}
	return p
}

// A tiny evaluator for the C expressions used in Plural-Forms.
type pluralNode interface{ eval(n int) int }

type pnN struct{}
type pnNum int
type pnUnary struct {
	op string
	x  pluralNode
}
type pnBin struct {
	op   string
	l, r pluralNode
}
type pnCond struct{ c, a, b pluralNode }

func (pnN) eval(n int) int   { return n }
func (v pnNum) eval(int) int { return int(v) }
func (u pnUnary) eval(n int) int {
	v := u.x.eval(n)
	if u.op == "!" {
		return b2i(v == 0)
	}
	return -v
}
func (c pnCond) eval(n int) int {
	if c.c.eval(n) != 0 {
		return c.a.eval(n)
	}
	return c.b.eval(n)
}
func (b pnBin) eval(n int) int {
	l := b.l.eval(n)
	switch b.op { // short-circuit first
	case "&&":
		return b2i(l != 0 && b.r.eval(n) != 0)
	case "||":
		return b2i(l != 0 || b.r.eval(n) != 0)
	}
	r := b.r.eval(n)
	switch b.op {
	case "==":
		return b2i(l == r)
	case "!=":
		return b2i(l != r)
	case "<":
		return b2i(l < r)
	case "<=":
		return b2i(l <= r)
	case ">":
		return b2i(l > r)
	case ">=":
		return b2i(l >= r)
	case "+":
		return l + r
	case "-":
		return l - r
	case "*":
		return l * r
	case "/":
		if r == 0 {
			return 0
		}
		return l / r
	case "%":
		if r == 0 {
			return 0
		}
		return l % r
	}
	return 0
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

type pluralParser struct {
	toks []string
	pos  int
	ok   bool
}

func parsePluralExpr(s string) (pluralNode, bool) {
	toks, ok := tokenizePlural(s)
	if !ok || len(toks) == 0 {
		return nil, false
	}
	p := &pluralParser{toks: toks, ok: true}
	node := p.cond()
	if !p.ok || p.pos != len(p.toks) {
		return nil, false
	}
	return node, true
}

func tokenizePlural(s string) ([]string, bool) {
	var toks []string
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\\':
			i++
		case c >= '0' && c <= '9':
			j := i
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		case c == 'n':
			toks = append(toks, "n")
			i++
		default:
			if i+1 < len(s) {
				two := s[i : i+2]
				switch two {
				case "==", "!=", "<=", ">=", "&&", "||":
					toks = append(toks, two)
					i += 2
					continue
				}
			}
			if strings.IndexByte("?:()<>!+-*/%", c) < 0 {
				return nil, false
			}
			toks = append(toks, string(c))
			i++
		}
	}
	return toks, true
}

func (p *pluralParser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *pluralParser) next() string {
	t := p.peek()
	p.pos++
	return t
}

func (p *pluralParser) cond() pluralNode {
	c := p.binary(0)
	if p.peek() == "?" {
		p.next()
		a := p.cond()
		if p.next() != ":" {
			p.ok = false
			return pnNum(0)
		}
		b := p.cond()
		return pnCond{c, a, b}
	}
	return c
}

// binary operators by precedence, lowest first
var pluralLevels = [][]string{{"||"}, {"&&"}, {"==", "!="}, {"<", "<=", ">", ">="}, {"+", "-"}, {"*", "/", "%"}}

func (p *pluralParser) binary(level int) pluralNode {
	if level == len(pluralLevels) {
		return p.unary()
	}
	l := p.binary(level + 1)
	for {
		op := p.peek()
		found := false
		for _, o := range pluralLevels[level] {
			if o == op {
				found = true
			}
		}
		if !found {
			return l
		}
		p.next()
		l = pnBin{op, l, p.binary(level + 1)}
	}
}

func (p *pluralParser) unary() pluralNode {
	switch p.peek() {
	case "!", "-":
		op := p.next()
		return pnUnary{op, p.unary()}
	case "(":
		p.next()
		x := p.cond()
		if p.next() != ")" {
			p.ok = false
		}
		return x
	case "n":
		p.next()
		return pnN{}
	}
	t := p.next()
	v, err := strconv.Atoi(t)
	if err != nil {
		p.ok = false
		return pnNum(0)
	}
	return pnNum(v)
}
