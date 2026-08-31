package user

import (
	"errors"
	"time"
)

var (
	ErrInvalidDateOfBirth = errors.New("date_of_birth must be a valid date in the past, formatted YYYY-MM-DD")
	ErrInvalidCountry     = errors.New("country must be a valid ISO 3166-1 alpha-2 code")
	ErrMissingIdentity    = errors.New("full_name, date_of_birth, and country are required")
)

// KYCIdentity holds the identity fields a KYC submission carries alongside
// the ID document itself. Previously read from the request and silently
// discarded rather than validated or persisted (nester#1190).
type KYCIdentity struct {
	FullName    string
	DateOfBirth time.Time
	Country     string
}

// ParseDateOfBirth parses a YYYY-MM-DD date string and rejects anything that
// isn't a real calendar date strictly in the past (an empty string, a
// malformed value, or a future date are all rejected the same way a client
// simply omitting the field should be — the endpoint must not accept and
// silently drop it, nor accept and silently store nonsense).
func ParseDateOfBirth(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, ErrInvalidDateOfBirth
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, ErrInvalidDateOfBirth
	}
	if !parsed.Before(time.Now()) {
		return time.Time{}, ErrInvalidDateOfBirth
	}
	return parsed, nil
}

// IsValidCountryCode reports whether code is a real ISO 3166-1 alpha-2
// country code (case-sensitive, uppercase — the canonical form). Checked
// against a fixed, real set rather than accepting any two-letter string, so
// a typo or a placeholder value is rejected at submission time instead of
// silently persisted as if it identified a real jurisdiction.
func IsValidCountryCode(code string) bool {
	_, ok := iso3166Alpha2Countries[code]
	return ok
}

// iso3166Alpha2Countries is the current set of assigned ISO 3166-1 alpha-2
// country codes (249 entries, including exceptionally reserved codes in
// active use such as GB, per the ISO 3166-1 maintenance agency's published
// list). Deliberately excludes withdrawn/unassigned codes.
var iso3166Alpha2Countries = map[string]struct{}{
	"AD": {}, "AE": {}, "AF": {}, "AG": {}, "AI": {}, "AL": {}, "AM": {}, "AO": {}, "AQ": {}, "AR": {},
	"AS": {}, "AT": {}, "AU": {}, "AW": {}, "AX": {}, "AZ": {}, "BA": {}, "BB": {}, "BD": {}, "BE": {},
	"BF": {}, "BG": {}, "BH": {}, "BI": {}, "BJ": {}, "BL": {}, "BM": {}, "BN": {}, "BO": {}, "BQ": {},
	"BR": {}, "BS": {}, "BT": {}, "BV": {}, "BW": {}, "BY": {}, "BZ": {}, "CA": {}, "CC": {}, "CD": {},
	"CF": {}, "CG": {}, "CH": {}, "CI": {}, "CK": {}, "CL": {}, "CM": {}, "CN": {}, "CO": {}, "CR": {},
	"CU": {}, "CV": {}, "CW": {}, "CX": {}, "CY": {}, "CZ": {}, "DE": {}, "DJ": {}, "DK": {}, "DM": {},
	"DO": {}, "DZ": {}, "EC": {}, "EE": {}, "EG": {}, "EH": {}, "ER": {}, "ES": {}, "ET": {}, "FI": {},
	"FJ": {}, "FK": {}, "FM": {}, "FO": {}, "FR": {}, "GA": {}, "GB": {}, "GD": {}, "GE": {}, "GF": {},
	"GG": {}, "GH": {}, "GI": {}, "GL": {}, "GM": {}, "GN": {}, "GP": {}, "GQ": {}, "GR": {}, "GS": {},
	"GT": {}, "GU": {}, "GW": {}, "GY": {}, "HK": {}, "HM": {}, "HN": {}, "HR": {}, "HT": {}, "HU": {},
	"ID": {}, "IE": {}, "IL": {}, "IM": {}, "IN": {}, "IO": {}, "IQ": {}, "IR": {}, "IS": {}, "IT": {},
	"JE": {}, "JM": {}, "JO": {}, "JP": {}, "KE": {}, "KG": {}, "KH": {}, "KI": {}, "KM": {}, "KN": {},
	"KP": {}, "KR": {}, "KW": {}, "KY": {}, "KZ": {}, "LA": {}, "LB": {}, "LC": {}, "LI": {}, "LK": {},
	"LR": {}, "LS": {}, "LT": {}, "LU": {}, "LV": {}, "LY": {}, "MA": {}, "MC": {}, "MD": {}, "ME": {},
	"MF": {}, "MG": {}, "MH": {}, "MK": {}, "ML": {}, "MM": {}, "MN": {}, "MO": {}, "MP": {}, "MQ": {},
	"MR": {}, "MS": {}, "MT": {}, "MU": {}, "MV": {}, "MW": {}, "MX": {}, "MY": {}, "MZ": {}, "NA": {},
	"NC": {}, "NE": {}, "NF": {}, "NG": {}, "NI": {}, "NL": {}, "NO": {}, "NP": {}, "NR": {}, "NU": {},
	"NZ": {}, "OM": {}, "PA": {}, "PE": {}, "PF": {}, "PG": {}, "PH": {}, "PK": {}, "PL": {}, "PM": {},
	"PN": {}, "PR": {}, "PS": {}, "PT": {}, "PW": {}, "PY": {}, "QA": {}, "RE": {}, "RO": {}, "RS": {},
	"RU": {}, "RW": {}, "SA": {}, "SB": {}, "SC": {}, "SD": {}, "SE": {}, "SG": {}, "SH": {}, "SI": {},
	"SJ": {}, "SK": {}, "SL": {}, "SM": {}, "SN": {}, "SO": {}, "SR": {}, "SS": {}, "ST": {}, "SV": {},
	"SX": {}, "SY": {}, "SZ": {}, "TC": {}, "TD": {}, "TF": {}, "TG": {}, "TH": {}, "TJ": {}, "TK": {},
	"TL": {}, "TM": {}, "TN": {}, "TO": {}, "TR": {}, "TT": {}, "TV": {}, "TW": {}, "TZ": {}, "UA": {},
	"UG": {}, "UM": {}, "US": {}, "UY": {}, "UZ": {}, "VA": {}, "VC": {}, "VE": {}, "VG": {}, "VI": {},
	"VN": {}, "VU": {}, "WF": {}, "WS": {}, "YE": {}, "YT": {}, "ZA": {}, "ZM": {}, "ZW": {},
}
