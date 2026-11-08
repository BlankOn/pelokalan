package po

import "sort"

// Language describes a target language and its default plural rule
type Language struct {
	Code        string
	Name        string // native name
	PluralForms string
}

const (
	pluralNone   = "nplurals=1; plural=0;"
	pluralTwo    = "nplurals=2; plural=(n != 1);"
	pluralFrench = "nplurals=2; plural=(n > 1);"
	pluralSlavic = "nplurals=3; plural=(n%10==1 && n%100!=11 ? 0 : n%10>=2 && n%10<=4 && (n%100<10 || n%100>=20) ? 1 : 2);"
	pluralCzech  = "nplurals=3; plural=(n==1) ? 0 : (n>=2 && n<=4) ? 1 : 2;"
	pluralPolish = "nplurals=3; plural=(n==1 ? 0 : n%10>=2 && n%10<=4 && (n%100<10 || n%100>=20) ? 1 : 2);"
	pluralArabic = "nplurals=6; plural=(n==0 ? 0 : n==1 ? 1 : n==2 ? 2 : n%100>=3 && n%100<=10 ? 3 : n%100>=11 ? 4 : 5);"
)

var languages = []Language{
	{"id", "Bahasa Indonesia", pluralNone},
	{"jv", "Basa Jawa", pluralNone},
	{"su", "Basa Sunda", pluralNone},
	{"ms", "Bahasa Melayu", pluralNone},
	{"en", "English", pluralTwo},
	{"en_GB", "English (UK)", pluralTwo},
	{"ar", "العربية", pluralArabic},
	{"cs", "Čeština", pluralCzech},
	{"da", "Dansk", pluralTwo},
	{"de", "Deutsch", pluralTwo},
	{"el", "Ελληνικά", pluralTwo},
	{"es", "Español", pluralTwo},
	{"fa", "فارسی", pluralFrench},
	{"fi", "Suomi", pluralTwo},
	{"fil", "Filipino", pluralFrench},
	{"fr", "Français", pluralFrench},
	{"he", "עברית", pluralTwo},
	{"hi", "हिन्दी", pluralTwo},
	{"hu", "Magyar", pluralTwo},
	{"it", "Italiano", pluralTwo},
	{"ja", "日本語", pluralNone},
	{"km", "ខ្មែរ", pluralNone},
	{"ko", "한국어", pluralNone},
	{"lo", "ລາວ", pluralNone},
	{"my", "မြန်မာ", pluralNone},
	{"nb", "Norsk bokmål", pluralTwo},
	{"nl", "Nederlands", pluralTwo},
	{"pl", "Polski", pluralPolish},
	{"pt", "Português", pluralTwo},
	{"pt_BR", "Português (Brasil)", pluralFrench},
	{"ro", "Română", "nplurals=3; plural=(n==1 ? 0 : (n==0 || (n%100 > 0 && n%100 < 20)) ? 1 : 2);"},
	{"ru", "Русский", pluralSlavic},
	{"sk", "Slovenčina", pluralCzech},
	{"sv", "Svenska", pluralTwo},
	{"th", "ไทย", pluralNone},
	{"tr", "Türkçe", pluralFrench},
	{"uk", "Українська", pluralSlavic},
	{"vi", "Tiếng Việt", pluralNone},
	{"zh_CN", "简体中文", pluralNone},
	{"zh_TW", "繁體中文", pluralNone},
}

var languageByCode = func() map[string]Language {
	m := make(map[string]Language, len(languages))
	for _, l := range languages {
		m[l.Code] = l
	}
	return m
}()

// Languages returns the known languages sorted by code
func Languages() []Language {
	out := append([]Language(nil), languages...)
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// LookupLanguage returns a known language by code. Unknown codes fall back to
// the base language (e.g. "de_AT" -> "de") and then to a two-form rule.
func LookupLanguage(code string) Language {
	if l, ok := languageByCode[code]; ok {
		return l
	}
	for i := 0; i < len(code); i++ {
		if code[i] == '_' || code[i] == '-' {
			if l, ok := languageByCode[code[:i]]; ok {
				return Language{Code: code, Name: l.Name, PluralForms: l.PluralForms}
			}
			break
		}
	}
	return Language{Code: code, Name: code, PluralForms: pluralTwo}
}

// LanguageName returns a display name for a language code
func LanguageName(code string) string {
	if code == "" {
		return ""
	}
	return LookupLanguage(code).Name
}
