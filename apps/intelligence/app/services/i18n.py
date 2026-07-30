"""Language preference, detection, financial glossary, and locale-aware
number/date formatting for Prometheus (#multilingual).

Design:
- Language is resolved once per request: an explicit user preference (set in
  settings, shared with the frontend i18n work in #789) is authoritative; when
  none is set, we fall back to detecting the language of the user's message.
- Prometheus is instructed to *generate natively* in the target language
  rather than writing English and translating it, using a small per-language
  glossary so financial terms ("compound interest", "yield", "locked
  savings") get their established local rendering instead of a literal
  mistranslation.
- All amounts, currencies, dates, and percentages are formatted
  deterministically here (not by the model) and handed to Prometheus as
  pre-formatted strings to reference verbatim. This keeps numbers trustworthy
  across languages and is meant to track the same locale conventions the
  frontend uses once #789 lands.

Adding a language: add one entry to SUPPORTED_LANGUAGES, FINANCIAL_GLOSSARY,
_NUMBER_FORMATS, and _MONTH_NAMES. Everything else (detection, directive
building, formatting) picks it up automatically.
"""

from __future__ import annotations

import datetime as _dt
import logging
import re
from typing import Optional

logger = logging.getLogger(__name__)

DEFAULT_LANGUAGE = "en"

# The languages Nester's AI layer currently supports natively. This is meant
# to track the locale set the frontend i18n work (#789) settles on — treat it
# as the Python-side half of that shared spec until the two are wired
# together, and keep this list and #789's locale list in sync when either
# changes.
SUPPORTED_LANGUAGES: dict[str, str] = {
    "en": "English",
    "fr": "French",
    "pt": "Portuguese",
    "sw": "Swahili",
    "ha": "Hausa",
    "yo": "Yoruba",
    "ar": "Arabic",
}


def is_supported_language(code: str) -> bool:
    return code.lower().strip() in SUPPORTED_LANGUAGES


def parse_language_preference(value: Optional[str]) -> Optional[str]:
    """Normalize an explicit language preference. Returns None if blank or unsupported."""
    if not value:
        return None
    code = value.strip().lower().split("-")[0]
    return code if is_supported_language(code) else None


# ---------------------------------------------------------------------------
# Financial glossary — established local terms, not literal translations.
# Reviewed starter set; native-speaker review is recommended before treating
# any single term as final, but these are chosen to avoid the most common
# literal-translation mistakes (e.g. "yield" is a return, not a harvest).
# ---------------------------------------------------------------------------

FINANCIAL_GLOSSARY: dict[str, dict[str, str]] = {
    "en": {
        "compound_interest": "compound interest",
        "yield": "yield",
        "savings_goal": "savings goal",
        "locked_savings": "locked savings",
        "vault": "vault",
        "deposit": "deposit",
        "withdrawal": "withdrawal",
        "apy": "APY (annual percentage yield)",
        "risk_tier": "risk tier",
    },
    "fr": {
        "compound_interest": "intérêts composés",
        "yield": "rendement",
        "savings_goal": "objectif d'épargne",
        "locked_savings": "épargne bloquée",
        "vault": "coffre",
        "deposit": "dépôt",
        "withdrawal": "retrait",
        "apy": "APY (rendement annuel)",
        "risk_tier": "niveau de risque",
    },
    "pt": {
        "compound_interest": "juros compostos",
        "yield": "rendimento",
        "savings_goal": "meta de poupança",
        "locked_savings": "poupança bloqueada",
        "vault": "cofre",
        "deposit": "depósito",
        "withdrawal": "levantamento",
        "apy": "APY (rendimento anual)",
        "risk_tier": "nível de risco",
    },
    "sw": {
        "compound_interest": "riba inayojumuika",
        "yield": "faida",
        "savings_goal": "lengo la akiba",
        "locked_savings": "akiba iliyofungwa",
        "vault": "hazina",
        "deposit": "amana",
        "withdrawal": "uondoaji",
        "apy": "APY (kiwango cha faida cha mwaka)",
        "risk_tier": "kiwango cha hatari",
    },
    "ha": {
        "compound_interest": "ribar da ke ƙaruwa",
        "yield": "riba",
        "savings_goal": "burin ajiya",
        "locked_savings": "ajiyar da aka kulle",
        "vault": "ma'ajin kuɗi",
        "deposit": "ajiya",
        "withdrawal": "cirewa",
        "apy": "APY (ribar shekara)",
        "risk_tier": "matakin haɗari",
    },
    "yo": {
        "compound_interest": "èlé àkójọpọ̀",
        "yield": "èrè",
        "savings_goal": "ète ìfowópamọ́",
        "locked_savings": "ìfowópamọ́ tí a ti dí",
        "vault": "àpótí ìfowópamọ́",
        "deposit": "ìsanwó",
        "withdrawal": "yíyọ owó",
        "apy": "APY (èlé ọdún)",
        "risk_tier": "ìpele ewu",
    },
    "ar": {
        "compound_interest": "الفائدة المركبة",
        "yield": "العائد",
        "savings_goal": "هدف الادخار",
        "locked_savings": "مدخرات مقفلة",
        "vault": "خزينة",
        "deposit": "إيداع",
        "withdrawal": "سحب",
        "apy": "العائد السنوي (APY)",
        "risk_tier": "مستوى المخاطرة",
    },
}


def glossary_for(language: str) -> dict[str, str]:
    return FINANCIAL_GLOSSARY.get(language, FINANCIAL_GLOSSARY[DEFAULT_LANGUAGE])


# ---------------------------------------------------------------------------
# Language detection (fallback only — an explicit preference always wins)
# ---------------------------------------------------------------------------

# Lightweight, dependency-free detector: a handful of unambiguous marker words
# per language plus a script check for Arabic. This is a fallback of last
# resort — it only needs to be good enough to route a message that arrives
# with no stored preference, not to be a general-purpose classifier.
_ARABIC_SCRIPT_RE = re.compile(r"[؀-ۿ]")

_MARKER_WORDS: dict[str, set[str]] = {
    "fr": {
        "le", "la", "les", "je", "vous", "et", "est", "pour", "avec",
        "combien", "épargne", "rendement", "compte", "merci",
    },
    "pt": {
        "o", "a", "os", "as", "você", "e", "é", "para", "com",
        "quanto", "poupança", "rendimento", "conta", "obrigado",
    },
    "sw": {
        "na", "ni", "kwa", "akiba", "faida", "pesa", "asante", "habari",
    },
    "ha": {
        "da", "ne", "ajiya", "kuɗi", "na", "yaya", "godiya",
    },
    "yo": {
        "ati", "mi", "owo", "ifowopamo", "bawo", "e se",
    },
}


def detect_language(text: Optional[str]) -> str:
    """Best-effort language detection from a single message.

    Returns a code from SUPPORTED_LANGUAGES, defaulting to DEFAULT_LANGUAGE
    when the text is empty, ambiguous, or in an unsupported language — the
    fallback is deliberately conservative rather than guessing.
    """
    if not text or not text.strip():
        return DEFAULT_LANGUAGE

    if _ARABIC_SCRIPT_RE.search(text):
        return "ar"

    words = set(re.findall(r"[a-zà-öø-ÿ']+", text.lower()))
    if not words:
        return DEFAULT_LANGUAGE

    best_lang = DEFAULT_LANGUAGE
    best_score = 0
    for lang, markers in _MARKER_WORDS.items():
        score = len(words & markers)
        if score > best_score:
            best_score = score
            best_lang = lang

    # Require at least two marker hits before trusting the guess over English —
    # a single shared word (e.g. "na") is too weak a signal on its own.
    return best_lang if best_score >= 2 else DEFAULT_LANGUAGE


def resolve_language(
    preference: Optional[str],
    sample_text: Optional[str] = None,
) -> str:
    """Resolve the language to respond in.

    An explicit, valid preference is authoritative and is never overridden by
    the incidental language of a single message. Only when no preference is
    stored do we fall back to detecting the language of sample_text.
    """
    explicit = parse_language_preference(preference)
    if explicit:
        return explicit
    return detect_language(sample_text)


# ---------------------------------------------------------------------------
# Deterministic locale formatting — amounts, percentages, dates.
#
# Numbers are formatted here, not by the model: Prometheus is instructed to
# reference these pre-formatted strings verbatim rather than reformat amounts
# itself, so a mis-rendered number is never a trust failure. This mirrors
# (and is intended to be kept in sync with) the frontend's locale formatting
# once #789 lands.
# ---------------------------------------------------------------------------


class _NumberFormat:
    __slots__ = ("decimal_sep", "group_sep", "symbol_prefix", "symbol_space")

    def __init__(
        self, decimal_sep: str, group_sep: str, symbol_prefix: bool, symbol_space: bool
    ) -> None:
        self.decimal_sep = decimal_sep
        self.group_sep = group_sep
        self.symbol_prefix = symbol_prefix
        self.symbol_space = symbol_space


_NUMBER_FORMATS: dict[str, _NumberFormat] = {
    "en": _NumberFormat(".", ",", symbol_prefix=True, symbol_space=False),
    "fr": _NumberFormat(",", " ", symbol_prefix=False, symbol_space=True),
    "pt": _NumberFormat(",", ".", symbol_prefix=True, symbol_space=True),
    "sw": _NumberFormat(".", ",", symbol_prefix=True, symbol_space=False),
    "ha": _NumberFormat(".", ",", symbol_prefix=True, symbol_space=False),
    "yo": _NumberFormat(".", ",", symbol_prefix=True, symbol_space=False),
    "ar": _NumberFormat("٫", "٬", symbol_prefix=False, symbol_space=True),
}

# Currency symbols/codes as displayed to the user. Crypto assets (USDC, XLM)
# keep their ticker everywhere — they aren't fiat symbols with local variants.
_CURRENCY_SYMBOLS: dict[str, str] = {
    "USD": "$",
    "NGN": "₦",
    "GHS": "GH₵",
    "KES": "KSh",
}


def _group_integer(int_part: str, group_sep: str) -> str:
    negative = int_part.startswith("-")
    digits = int_part[1:] if negative else int_part
    groups = []
    while len(digits) > 3:
        groups.insert(0, digits[-3:])
        digits = digits[:-3]
    groups.insert(0, digits)
    out = group_sep.join(groups)
    return f"-{out}" if negative else out


def format_amount(
    amount: float, currency: str, language: str, decimals: int = 2
) -> str:
    """Format a currency amount per the target language's number conventions.

    Crypto tickers (USDC, XLM, ...) are rendered as "<number> <TICKER>";
    recognised fiat codes use their local symbol with the language's
    prefix/suffix placement.
    """
    fmt = _NUMBER_FORMATS.get(language, _NUMBER_FORMATS[DEFAULT_LANGUAGE])
    rounded = round(float(amount), decimals)
    sign = "-" if rounded < 0 else ""
    whole, _, frac = f"{abs(rounded):.{decimals}f}".partition(".")
    number = _group_integer(whole, fmt.group_sep)
    if decimals > 0:
        number = f"{number}{fmt.decimal_sep}{frac}"
    number = f"{sign}{number}"

    code = currency.upper().strip()
    symbol = _CURRENCY_SYMBOLS.get(code)
    if symbol is None:
        # Crypto or unrecognised code — ticker suffix, e.g. "1,250.00 USDC".
        return f"{number} {code}".strip()

    if fmt.symbol_prefix:
        return f"{symbol}{number}" if not fmt.symbol_space else f"{symbol} {number}"
    return f"{number} {symbol}" if fmt.symbol_space else f"{number}{symbol}"


def format_percentage(value: float, language: str, decimals: int = 2) -> str:
    fmt = _NUMBER_FORMATS.get(language, _NUMBER_FORMATS[DEFAULT_LANGUAGE])
    whole, _, frac = f"{abs(round(float(value), decimals)):.{decimals}f}".partition(".")
    number = _group_integer(whole, fmt.group_sep)
    if decimals > 0:
        number = f"{number}{fmt.decimal_sep}{frac}"
    sign = "-" if value < 0 else ""
    percent_sign = "٪" if language == "ar" else "%"
    return f"{sign}{number}{percent_sign}"


_MONTH_NAMES: dict[str, list[str]] = {
    "en": [
        "January", "February", "March", "April", "May", "June",
        "July", "August", "September", "October", "November", "December",
    ],
    "fr": [
        "janvier", "février", "mars", "avril", "mai", "juin",
        "juillet", "août", "septembre", "octobre", "novembre", "décembre",
    ],
    "pt": [
        "janeiro", "fevereiro", "março", "abril", "maio", "junho",
        "julho", "agosto", "setembro", "outubro", "novembro", "dezembro",
    ],
    "sw": [
        "Januari", "Februari", "Machi", "Aprili", "Mei", "Juni",
        "Julai", "Agosti", "Septemba", "Oktoba", "Novemba", "Desemba",
    ],
    "ha": [
        "Janairu", "Fabrairu", "Maris", "Afrilu", "Mayu", "Yuni",
        "Yuli", "Agusta", "Satumba", "Oktoba", "Nuwamba", "Disamba",
    ],
    "yo": [
        "Jánúárì", "Fẹ́bùárì", "Máàsì", "Épírì", "Méè", "Júùnù",
        "Júláì", "Ọ́gọ́sìtì", "Sẹ́tẹ̀mbà", "Ọ̀kùdù", "Nòfẹ́mbà", "Dísẹ̀mbà",
    ],
    "ar": [
        "يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو",
        "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر",
    ],
}


def format_date(value: "_dt.date | _dt.datetime | str", language: str) -> str:
    """Format a date as "<day> <Month> <year>" using the language's month names.

    Accepts a date/datetime or an ISO-8601 string (as commonly stored on
    savings goals / deadlines). Falls back to the raw string if parsing fails.
    """
    if isinstance(value, str):
        try:
            parsed = _dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
        except ValueError:
            return value
    else:
        parsed = value

    months = _MONTH_NAMES.get(language, _MONTH_NAMES[DEFAULT_LANGUAGE])
    return f"{parsed.day} {months[parsed.month - 1]} {parsed.year}"


# ---------------------------------------------------------------------------
# System prompt directive
# ---------------------------------------------------------------------------


def language_directive(language: str) -> str:
    """Build the system-prompt block that makes a response multilingual.

    Instructs the model to generate natively in the target language (not
    translate from English), gives it the established local terms for key
    financial concepts, and reiterates that every guardrail — grounding,
    refusal-on-absence, injection-safety, non-advice framing — applies
    identically regardless of language.
    """
    if language == DEFAULT_LANGUAGE:
        return (
            "## Language\n"
            "Respond in English."
        )

    language_name = SUPPORTED_LANGUAGES.get(language, language)
    glossary = glossary_for(language)
    glossary_lines = "\n".join(
        f'- "{key.replace("_", " ")}" → {term}' for key, term in glossary.items()
    )

    return f"""## Language
Respond natively in {language_name} ({language}). Write the entire response in
{language_name} — do not draft it in English and translate, and do not mix in
English sentences. Generating directly in {language_name} produces more
natural, idiomatic output than a literal translation, especially for
financial concepts that have an established local rendering rather than a
word-for-word one.

Use these established {language_name} financial terms exactly as given below
instead of inventing your own translation:
{glossary_lines}

Every amount, currency, date, and percentage in the conversation context has
already been formatted for this locale. Reference those pre-formatted values
exactly as given rather than reformatting or recalculating them yourself —
do not invent your own number formatting.

All of your guardrails apply identically in {language_name}: never state a
number that isn't grounded in the provided context, say plainly in
{language_name} when information is unavailable rather than guessing, and
keep the non-custodial, no-guarantee risk framing intact and clearly worded
in {language_name}."""


_PORTFOLIO_NARRATIVE_TEMPLATES: dict[str, str] = {
    "en": (
        "Your Nester portfolio has a total value of {total_value} across "
        "{vault_count} vault(s). Over the last 30 days, you've earned "
        "{yield_earned} in yield. Your portfolio risk level is {risk_level}. "
        "{top_recommendation}"
    ),
    "fr": (
        "Votre portefeuille Nester a une valeur totale de {total_value} "
        "répartie sur {vault_count} coffre(s). Au cours des 30 derniers "
        "jours, vous avez gagné {yield_earned} de rendement. Le niveau de "
        "risque de votre portefeuille est {risk_level}. {top_recommendation}"
    ),
    "pt": (
        "A sua carteira Nester tem um valor total de {total_value} em "
        "{vault_count} cofre(s). Nos últimos 30 dias, você ganhou "
        "{yield_earned} em rendimento. O nível de risco da sua carteira é "
        "{risk_level}. {top_recommendation}"
    ),
    "sw": (
        "Mkoba wako wa Nester una thamani ya jumla ya {total_value} katika "
        "hazina {vault_count}. Katika siku 30 zilizopita, umepata "
        "{yield_earned} kama faida. Kiwango cha hatari cha mkoba wako ni "
        "{risk_level}. {top_recommendation}"
    ),
    "ha": (
        "Jarin ku na Nester yana da jimlar darajar {total_value} a cikin "
        "ma'aji {vault_count}. A cikin kwanaki 30 da suka gabata, kun samu "
        "riba na {yield_earned}. Matakin haɗarin jarin ku shine {risk_level}. "
        "{top_recommendation}"
    ),
    "yo": (
        "Ìfowópamọ́ Nester rẹ ní iye lápapọ̀ {total_value} kọjá àpótí "
        "ìfowópamọ́ {vault_count}. Ní ọjọ́ 30 sẹ́yìn, o ti jèrè {yield_earned} "
        "gẹ́gẹ́ bí èrè. Ìpeleewu ìfowópamọ́ rẹ jẹ́ {risk_level}. "
        "{top_recommendation}"
    ),
    "ar": (
        "تبلغ القيمة الإجمالية لمحفظتك في Nester {total_value} عبر "
        "{vault_count} خزينة. على مدى الثلاثين يومًا الماضية، حققت عائدًا "
        "قدره {yield_earned}. مستوى المخاطرة لمحفظتك هو {risk_level}. "
        "{top_recommendation}"
    ),
}


def build_portfolio_narrative(
    total_value: str,
    vault_count: int,
    yield_earned: str,
    risk_level: str,
    top_recommendation: str,
    language: str,
) -> str:
    """Build the fixed-shape portfolio narrative sentence in `language`,
    with the AI-generated `top_recommendation` (already in that language)
    appended.
    """
    template = _PORTFOLIO_NARRATIVE_TEMPLATES.get(
        language, _PORTFOLIO_NARRATIVE_TEMPLATES[DEFAULT_LANGUAGE]
    )
    return template.format(
        total_value=total_value,
        vault_count=vault_count,
        yield_earned=yield_earned,
        risk_level=risk_level,
        top_recommendation=top_recommendation,
    )


def localize_system_prompt(base_system: str, language: str) -> str:
    """Append the language directive to a base system prompt."""
    return f"{base_system}\n\n{language_directive(language)}"


def structured_output_language_note(language: str) -> str:
    """Instruction appended to JSON-schema prompts (analyze/recommend endpoints).

    These prompts ask Claude to return JSON matching a fixed schema — the
    keys, enum values, and field names must stay in English so callers can
    keep parsing them programmatically, but the natural-language string
    values (rationale, commentary, summaries, nudges, ...) should still be
    generated natively in the user's language.
    """
    if language == DEFAULT_LANGUAGE:
        return ""
    language_name = SUPPORTED_LANGUAGES.get(language, language)
    return (
        f" Write every natural-language string value (rationale, commentary, "
        f"summary, nudges, progress assessment, disclaimer, etc.) natively in "
        f"{language_name}, using the established {language_name} financial "
        f"terms rather than literal translations. Keep JSON keys and enum "
        f"values (e.g. \"confidence\", \"risk_level\") exactly as specified "
        f"in the schema, in English."
    )
