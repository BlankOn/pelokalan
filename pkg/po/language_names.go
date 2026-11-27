package po

import (
	"sort"
	"strings"
)

// LanguageNameEntry is a translation language offered in pickers: its gettext code and its name in
// that language. Plural rules stay in the languages table (LookupLanguage).
type LanguageNameEntry struct {
	Code string
	Name string
}

// languageNames lists the languages a project can translate into (codes as gettext and the
// GNOME teams use them)
var languageNames = []LanguageNameEntry{
	{"ab", "Аҧсшәа"}, {"af", "Afrikaans"}, {"ak", "Akan"}, {"am", "አማርኛ"}, {"an", "Aragonés"},
	{"ar", "العربية"}, {"as", "অসমীয়া"}, {"ast", "Asturianu"}, {"az", "Azərbaycan dili"},
	{"ba", "Башҡорт теле"}, {"bal", "بلوچی"}, {"ban", "Basa Bali"}, {"be", "Беларуская"},
	{"be@latin", "Biełaruskaja (łacinka)"}, {"bem", "Ichibemba"}, {"bg", "Български"},
	{"bho", "भोजपुरी"}, {"bjn", "Bahasa Banjar"}, {"bn", "বাংলা"}, {"bn_IN", "বাংলা (ভারত)"},
	{"bo", "བོད་ཡིག"}, {"br", "Brezhoneg"}, {"brx", "बड़ो"}, {"bs", "Bosanski"}, {"bug", "Basa Ugi"},
	{"ca", "Català"}, {"ca@valencia", "Català (valencià)"}, {"ce", "Нохчийн"}, {"ckb", "کوردیی ناوەندی"},
	{"co", "Corsu"}, {"crh", "Qırımtatar tili"}, {"cs", "Čeština"}, {"csb", "Kaszëbsczi"},
	{"cv", "Чӑвашла"}, {"cy", "Cymraeg"}, {"da", "Dansk"}, {"de", "Deutsch"}, {"de_CH", "Deutsch (Schweiz)"},
	{"doi", "डोगरी"}, {"dsb", "Dolnoserbšćina"}, {"dv", "ދިވެހި"}, {"dz", "རྫོང་ཁ"}, {"el", "Ελληνικά"},
	{"en_AU", "English (Australia)"}, {"en_CA", "English (Canada)"}, {"en_GB", "English (United Kingdom)"},
	{"en@shaw", "English (Shavian)"}, {"eo", "Esperanto"}, {"es", "Español"}, {"es_AR", "Español (Argentina)"},
	{"es_MX", "Español (México)"}, {"et", "Eesti"}, {"eu", "Euskara"}, {"fa", "فارسی"}, {"ff", "Fulfulde"},
	{"fi", "Suomi"}, {"fil", "Filipino"}, {"fo", "Føroyskt"}, {"fr", "Français"}, {"fr_CA", "Français (Canada)"},
	{"fur", "Furlan"}, {"fy", "Frysk"}, {"ga", "Gaeilge"}, {"gd", "Gàidhlig"}, {"gl", "Galego"},
	{"gn", "Avañe'ẽ"}, {"gu", "ગુજરાતી"}, {"gv", "Gaelg"}, {"ha", "Hausa"}, {"haw", "ʻŌlelo Hawaiʻi"},
	{"he", "עברית"}, {"hi", "हिन्दी"}, {"hne", "छत्तीसगढ़ी"}, {"hr", "Hrvatski"}, {"hsb", "Hornjoserbšćina"},
	{"ht", "Kreyòl ayisyen"}, {"hu", "Magyar"}, {"hy", "Հայերեն"}, {"ia", "Interlingua"},
	{"id", "Bahasa Indonesia"}, {"ie", "Interlingue"}, {"ig", "Asụsụ Igbo"}, {"io", "Ido"}, {"is", "Íslenska"},
	{"it", "Italiano"}, {"iu", "ᐃᓄᒃᑎᑐᑦ"}, {"ja", "日本語"}, {"jbo", "La .lojban."}, {"jv", "Basa Jawa"},
	{"ka", "ქართული"}, {"kab", "Taqbaylit"}, {"kk", "Қазақ тілі"}, {"kl", "Kalaallisut"}, {"km", "ខ្មែរ"},
	{"kmr", "Kurmancî"}, {"kn", "ಕನ್ನಡ"}, {"ko", "한국어"}, {"kok", "कोंकणी"}, {"ks", "कॉशुर"},
	{"ku", "Kurdî"}, {"kw", "Kernewek"}, {"ky", "Кыргызча"}, {"la", "Latina"}, {"lb", "Lëtzebuergesch"},
	{"lg", "Luganda"}, {"li", "Limburgs"}, {"lij", "Ligure"}, {"ln", "Lingála"}, {"lo", "ລາວ"},
	{"lt", "Lietuvių"}, {"ltg", "Latgalīšu"}, {"lv", "Latviešu"}, {"mad", "Basa Madhura"}, {"mai", "मैथिली"},
	{"mak", "Basa Mangkasara'"}, {"mg", "Malagasy"}, {"mhr", "Олык марий"}, {"mi", "Te Reo Māori"},
	{"min", "Baso Minangkabau"}, {"mk", "Македонски"}, {"ml", "മലയാളം"}, {"mn", "Монгол"},
	{"mni", "মৈতৈলোন্"}, {"mr", "मराठी"}, {"ms", "Bahasa Melayu"}, {"mt", "Malti"}, {"my", "မြန်မာ"},
	{"nah", "Nāhuatl"}, {"nap", "Napulitano"}, {"nb", "Norsk bokmål"}, {"nds", "Plattdüütsch"},
	{"ne", "नेपाली"}, {"nl", "Nederlands"}, {"nn", "Norsk nynorsk"}, {"nso", "Sesotho sa Leboa"},
	{"oc", "Occitan"}, {"or", "ଓଡ଼ିଆ"}, {"os", "Ирон"}, {"pa", "ਪੰਜਾਬੀ"}, {"pl", "Polski"}, {"ps", "پښتو"},
	{"pt", "Português"}, {"pt_BR", "Português do Brasil"}, {"qu", "Runa Simi"}, {"rm", "Rumantsch"},
	{"ro", "Română"}, {"ru", "Русский"}, {"rw", "Ikinyarwanda"}, {"sa", "संस्कृतम्"}, {"sah", "Саха тыла"},
	{"sat", "ᱥᱟᱱᱛᱟᱲᱤ"}, {"sc", "Sardu"}, {"sd", "سنڌي"}, {"se", "Davvisámegiella"}, {"si", "සිංහල"},
	{"sk", "Slovenčina"}, {"sl", "Slovenščina"}, {"sm", "Gagana Sāmoa"}, {"so", "Soomaali"}, {"sq", "Shqip"},
	{"sr", "Српски"}, {"sr@latin", "Srpski (latinica)"}, {"ss", "SiSwati"}, {"st", "Sesotho"},
	{"su", "Basa Sunda"}, {"sv", "Svenska"}, {"sw", "Kiswahili"}, {"szl", "Ślōnski"}, {"ta", "தமிழ்"},
	{"te", "తెలుగు"}, {"tet", "Tetun"}, {"tg", "Тоҷикӣ"}, {"th", "ไทย"}, {"ti", "ትግርኛ"}, {"tk", "Türkmençe"},
	{"tl", "Tagalog"}, {"tn", "Setswana"}, {"to", "Lea faka-Tonga"}, {"tr", "Türkçe"}, {"ts", "Xitsonga"},
	{"tt", "Татарча"}, {"ug", "ئۇيغۇرچە"}, {"uk", "Українська"}, {"ur", "اردو"}, {"uz", "Oʻzbekcha"},
	{"uz@cyrillic", "Ўзбекча"}, {"ve", "Tshivenḓa"}, {"vec", "Vèneto"}, {"vi", "Tiếng Việt"},
	{"wa", "Walon"}, {"wo", "Wolof"}, {"xh", "isiXhosa"}, {"yi", "ייִדיש"}, {"yo", "Yorùbá"},
	{"zgh", "ⵜⴰⵎⴰⵣⵉⵖⵜ"}, {"zh_CN", "中文（简体）"}, {"zh_HK", "中文（香港）"}, {"zh_TW", "中文（正體）"},
	{"zu", "isiZulu"},
}

// LanguageNames returns every language a project can translate into, sorted by code
func LanguageNames() []LanguageNameEntry {
	out := append([]LanguageNameEntry(nil), languageNames...)
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// NativeLanguageName returns a language's name in that language ("" when it is not listed)
func NativeLanguageName(code string) string {
	for _, l := range languageNames {
		if l.Code == code {
			return l.Name
		}
	}
	return ""
}

// SameLanguage compares language codes the way PO files write them: "pt-BR", "pt_BR" and
// "pt_br" are the same language, "pt" and "pt_BR" are not
func SameLanguage(a, b string) bool {
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", "_")) }
	return norm(a) == norm(b)
}
