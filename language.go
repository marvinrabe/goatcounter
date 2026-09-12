package goatcounter

import (
	"strconv"
	"strings"
)

// AcceptLanguage gets the ISO 639-3 code for the language with the highest
// quality value in an Accept-Language header, or nil if the header is empty or
// has no language we know about.
//
// Only the language itself is stored, not the region or script: "nl-BE" and
// "nl" are both recorded as "nld".
func AcceptLanguage(header string) *string {
	var (
		best  string
		bestQ float64
	)
	for tag := range strings.SplitSeq(header, ",") {
		tag, qs, hasQ := strings.Cut(tag, ";")

		q := 1.0
		if hasQ {
			var err error
			qs = strings.ToLower(strings.TrimSpace(qs))
			q, err = strconv.ParseFloat(strings.TrimPrefix(qs, "q="), 64)
			if err != nil || q <= 0 {
				continue
			}
		}
		if q <= bestQ {
			continue
		}

		// Only keep the language subtag: "nl-BE" → "nl".
		lang, _, _ := strings.Cut(strings.TrimSpace(tag), "-")
		lang = strings.ToLower(lang)

		switch len(lang) {
		case 2:
			l, ok := iso639_1to3[lang]
			if !ok {
				continue
			}
			best, bestQ = l, q
		case 3: // Languages without a two-letter code are sent as-is.
			best, bestQ = lang, q
		default: // Including "*", which tells us nothing.
			continue
		}
	}

	if best == "" {
		return nil
	}
	return &best
}

// Two-letter ISO 639-1 codes as sent by browsers, mapped to the ISO 639-3 codes
// in the languages table:
//
//	curl -s 'https://salsa.debian.org/iso-codes-team/iso-codes/-/raw/main/data/iso_639-3.json' |
//		jq -r '."639-3" | .[] | select(.alpha_2) | "\t\"" + .alpha_2 + "\": \"" + .alpha_3 + "\","'
//
// Plus the deprecated codes below, which some browsers still send.
var iso639_1to3 = map[string]string{
	"in": "ind", // Replaced by "id".
	"iw": "heb", // Replaced by "he".
	"ji": "yid", // Replaced by "yi".
	"mo": "ron", // Replaced by "ro".

	"aa": "aar",
	"ab": "abk",
	"ae": "ave",
	"af": "afr",
	"ak": "aka",
	"am": "amh",
	"an": "arg",
	"ar": "ara",
	"as": "asm",
	"av": "ava",
	"ay": "aym",
	"az": "aze",
	"ba": "bak",
	"be": "bel",
	"bg": "bul",
	"bi": "bis",
	"bm": "bam",
	"bn": "ben",
	"bo": "bod",
	"br": "bre",
	"bs": "bos",
	"ca": "cat",
	"ce": "che",
	"ch": "cha",
	"co": "cos",
	"cr": "cre",
	"cs": "ces",
	"cu": "chu",
	"cv": "chv",
	"cy": "cym",
	"da": "dan",
	"de": "deu",
	"dv": "div",
	"dz": "dzo",
	"ee": "ewe",
	"el": "ell",
	"en": "eng",
	"eo": "epo",
	"es": "spa",
	"et": "est",
	"eu": "eus",
	"fa": "fas",
	"ff": "ful",
	"fi": "fin",
	"fj": "fij",
	"fo": "fao",
	"fr": "fra",
	"fy": "fry",
	"ga": "gle",
	"gd": "gla",
	"gl": "glg",
	"gn": "grn",
	"gu": "guj",
	"gv": "glv",
	"ha": "hau",
	"he": "heb",
	"hi": "hin",
	"ho": "hmo",
	"hr": "hrv",
	"ht": "hat",
	"hu": "hun",
	"hy": "hye",
	"hz": "her",
	"ia": "ina",
	"id": "ind",
	"ie": "ile",
	"ig": "ibo",
	"ii": "iii",
	"ik": "ipk",
	"io": "ido",
	"is": "isl",
	"it": "ita",
	"iu": "iku",
	"ja": "jpn",
	"jv": "jav",
	"ka": "kat",
	"kg": "kon",
	"ki": "kik",
	"kj": "kua",
	"kk": "kaz",
	"kl": "kal",
	"km": "khm",
	"kn": "kan",
	"ko": "kor",
	"kr": "kau",
	"ks": "kas",
	"ku": "kur",
	"kv": "kom",
	"kw": "cor",
	"ky": "kir",
	"la": "lat",
	"lb": "ltz",
	"lg": "lug",
	"li": "lim",
	"ln": "lin",
	"lo": "lao",
	"lt": "lit",
	"lu": "lub",
	"lv": "lav",
	"mg": "mlg",
	"mh": "mah",
	"mi": "mri",
	"mk": "mkd",
	"ml": "mal",
	"mn": "mon",
	"mr": "mar",
	"ms": "msa",
	"mt": "mlt",
	"my": "mya",
	"na": "nau",
	"nb": "nob",
	"nd": "nde",
	"ne": "nep",
	"ng": "ndo",
	"nl": "nld",
	"nn": "nno",
	"no": "nor",
	"nr": "nbl",
	"nv": "nav",
	"ny": "nya",
	"oc": "oci",
	"oj": "oji",
	"om": "orm",
	"or": "ori",
	"os": "oss",
	"pa": "pan",
	"pi": "pli",
	"pl": "pol",
	"ps": "pus",
	"pt": "por",
	"qu": "que",
	"rm": "roh",
	"rn": "run",
	"ro": "ron",
	"ru": "rus",
	"rw": "kin",
	"sa": "san",
	"sc": "srd",
	"sd": "snd",
	"se": "sme",
	"sg": "sag",
	"sh": "hbs",
	"si": "sin",
	"sk": "slk",
	"sl": "slv",
	"sm": "smo",
	"sn": "sna",
	"so": "som",
	"sq": "sqi",
	"sr": "srp",
	"ss": "ssw",
	"st": "sot",
	"su": "sun",
	"sv": "swe",
	"sw": "swa",
	"ta": "tam",
	"te": "tel",
	"tg": "tgk",
	"th": "tha",
	"ti": "tir",
	"tk": "tuk",
	"tl": "tgl",
	"tn": "tsn",
	"to": "ton",
	"tr": "tur",
	"ts": "tso",
	"tt": "tat",
	"tw": "twi",
	"ty": "tah",
	"ug": "uig",
	"uk": "ukr",
	"ur": "urd",
	"uz": "uzb",
	"ve": "ven",
	"vi": "vie",
	"vo": "vol",
	"wa": "wln",
	"wo": "wol",
	"xh": "xho",
	"yi": "yid",
	"yo": "yor",
	"za": "zha",
	"zh": "zho",
	"zu": "zul",
}
