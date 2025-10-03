import os
import re
import csv
import json
import time
import random
import urllib.parse
from typing import List, Tuple, Dict, Optional, Any
from urllib.parse import urlparse

import requests
from bs4 import BeautifulSoup
import pandas as pd

from dotenv import load_dotenv
from openai import OpenAI

# ─────────────────────────────────────────────────────────────────────────────
# Konfiguration
# ─────────────────────────────────────────────────────────────────────────────
load_dotenv()
client = OpenAI()

INPUT_CSV = "list_of_names_and_affiliations.csv"   # zweispaltig: Name | Institution (ohne Header)
OUTPUT_CSV = "emails_ai_agent.csv"                 # 3 Spalten: Name | Institution | E-Mail
RUNS_LOG = "runs.jsonl"                            # strukturierte Schritt-Logs

MEM_DOMAINS = "memory_domains.json"                # {institution: {"domains": [...], "directory_hints": [...]} }
MEM_PATTERNS = "memory_patterns.json"              # {domain: {"patterns": ["first.last", "f.last", ...], "examples":[{"name":..,"email":..}] } }

USER_AGENT = (
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)
REQUEST_TIMEOUT = 15

# Budget / Limits
MAX_SEARCH_RESULTS = 10
MAX_PAGES_TO_SCAN = 8       # harte Obergrenze pro Person (Sense-Act Loop)
SLEEP_BETWEEN_REQUESTS = (1.0, 2.5)
JUDGE_VOTES = 3             # Self-Consistency
ACCEPT_CONF_PERSONAL = 0.60
ACCEPT_CONF_ROLE = 0.80
ACCEPT_CONF_UNCERTAIN = 0.85

# ─────────────────────────────────────────────────────────────────────────────
# Utility: CSV I/O
# ─────────────────────────────────────────────────────────────────────────────
def read_two_col_csv(path: str) -> List[Tuple[str, str]]:
    rows: List[Tuple[str, str]] = []
    with open(path, "r", encoding="utf-8", newline="") as f:
        r = csv.reader(f)
        for line in r:
            if not line:
                continue
            name = (line[0] or "").strip()
            inst = (line[1] or "").strip() if len(line) > 1 else ""
            if name:
                rows.append((name, inst))
    return rows

def write_results_csv(rows: List[Dict[str, str]], path: str) -> None:
    # exakt 3 Spalten
    df = pd.DataFrame(rows, columns=["Name", "Institution", "E-Mail"])
    df.to_csv(path, index=False)

# ─────────────────────────────────────────────────────────────────────────────
# Utility: Memory
# ─────────────────────────────────────────────────────────────────────────────
def load_json(path: str, default: Any) -> Any:
    try:
        if os.path.exists(path):
            with open(path, "r", encoding="utf-8") as f:
                return json.load(f)
    except Exception:
        pass
    return default

def save_json(path: str, data: Any) -> None:
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
    os.replace(tmp, path)

def memo_domains_add(memory: dict, institution: str, domain_or_hint: str) -> None:
    if not institution:
        return
    entry = memory.setdefault(institution, {"domains": [], "directory_hints": []})
    if "." in domain_or_hint:
        if domain_or_hint not in entry["domains"]:
            entry["domains"].append(domain_or_hint)
    else:
        if domain_or_hint not in entry["directory_hints"]:
            entry["directory_hints"].append(domain_or_hint)

def memo_patterns_add(memory: dict, email: str, name: str) -> None:
    try:
        local, domain = email.split("@", 1)
    except ValueError:
        return
    dom_entry = memory.setdefault(domain.lower(), {"patterns": [], "examples": []})
    # einfache Musterklassifikation
    toks = tokenize_name(name)
    pat = infer_pattern(local, toks)
    if pat and pat not in dom_entry["patterns"]:
        dom_entry["patterns"].append(pat)
    ex = {"name": name, "email": email}
    if ex not in dom_entry["examples"]:
        dom_entry["examples"].append(ex)

def infer_pattern(local: str, toks: List[str]) -> Optional[str]:
    """Grobe Musterkennung für den lokalen Teil; rein informativ für Memory."""
    local_l = local.lower()
    if not toks:
        return None
    first = toks[0]
    last = toks[-1]
    initials = "".join(t[0] for t in toks if t)

    if f"{first}.{last}" == local_l: return "first.last"
    if f"{first}{last}" == local_l: return "firstlast"
    if f"{first[0]}.{last}" == local_l: return "f.last"
    if f"{first[0]}{last}" == local_l: return "flast"
    if f"{last}.{first}" == local_l: return "last.first"
    if f"{last}{first}" == local_l: return "lastfirst"
    if f"{initials}{last}" == local_l: return "initials_last"
    if last in local_l and first[0] in local_l: return "contains_last_and_fi"
    if last in local_l: return "contains_last"
    return None

# ─────────────────────────────────────────────────────────────────────────────
# Utility: Logging
# ─────────────────────────────────────────────────────────────────────────────
def log_jsonl(path: str, record: dict) -> None:
    record = dict(record)
    record["ts"] = time.time()
    with open(path, "a", encoding="utf-8") as f:
        f.write(json.dumps(record, ensure_ascii=False) + "\n")

# ─────────────────────────────────────────────────────────────────────────────
# Suche / Fetch (neutrale Werkzeuge, keine Blacklists)
# ─────────────────────────────────────────────────────────────────────────────
def ddg_search(query: str, max_results: int = MAX_SEARCH_RESULTS) -> List[str]:
    """HTML-Variante von DuckDuckGo – dient dem Agenten als Fetch-Tool.
    (Wir lassen GPT die Queries/Seed-Links planen; DDG ist nur die ausführende Suche.)"""
    url = "https://html.duckduckgo.com/html/?q=" + urllib.parse.quote(query)
    headers = {"User-Agent": USER_AGENT}
    resp = requests.get(url, headers=headers, timeout=REQUEST_TIMEOUT)
    resp.raise_for_status()
    soup = BeautifulSoup(resp.text, "html.parser")

    urls: List[str] = []
    for a in soup.select(".result__a"):
        href = a.get("href") or ""
        parsed = urllib.parse.urlparse(href)
        q = urllib.parse.parse_qs(parsed.query)
        uddg = q.get("uddg", [""])[0]
        if uddg.startswith("http"):
            urls.append(uddg)
        if len(urls) >= max_results:
            break
    return urls

def fetch_page(url: str) -> Tuple[str, str]:
    headers = {"User-Agent": USER_AGENT, "Accept-Language": "en,de;q=0.9"}
    resp = requests.get(url, headers=headers, timeout=REQUEST_TIMEOUT, allow_redirects=True)
    resp.raise_for_status()
    html = resp.text
    soup = BeautifulSoup(html, "html.parser")
    for tag in soup(["script", "style", "noscript"]):
        tag.decompose()
    text = soup.get_text(" ", strip=True)
    return text, html

# ─────────────────────────────────────────────────────────────────────────────
# Extraktion
# ─────────────────────────────────────────────────────────────────────────────
RE_EMAIL = re.compile(r"(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b")
RE_FRAG = re.compile(r"(?i)([a-z0-9._%+\-]+)\s*(?:\n|\r|\s)*@(?:\n|\r|\s)*([a-z0-9.\-]+\.[a-z]{2,})")
RE_SYMBOLIC = re.compile(
    r"(?i)([a-z0-9._%+\-]+)\s*(?:\[?at\]?|\(at\)|\sat\s| at )\s*([a-z0-9.\-\s]+)"
    r"(?:\[?dot\]?|\(dot\)|\.| dot )([a-z]{2,})"
)

def sanitize_email(raw: str) -> str:
    email = (raw or "").strip()
    email = email.strip("<>\"' .,;:()[]")
    email = email.replace("mailto:", "").replace(" ", "")
    if "@" not in email or email.count("@") != 1:
        return ""
    if len(email) > 100:
        return ""
    return email if RE_EMAIL.fullmatch(email) else ""

def extract_emails_from_text_and_html(text: str, html: str) -> List[str]:
    found = set()
    # direkte
    for m in RE_EMAIL.findall(text):
        clean = sanitize_email(m)
        if clean: found.add(clean)
    # fragmentierte
    for u, d in RE_FRAG.findall(html):
        clean = sanitize_email(f"{u}@{d}")
        if clean: found.add(clean)
    # symbolische
    for user, dom, tld in RE_SYMBOLIC.findall(text + " " + html):
        domain = re.sub(r"\s+", "", dom)
        domain = domain.replace("(dot)", ".").replace("[dot]", ".").replace(" dot ", ".").replace("DOT", ".")
        clean = sanitize_email(f"{user}@{domain}.{tld}")
        if clean: found.add(clean)
    return list(found)

# ─────────────────────────────────────────────────────────────────────────────
# Features für die Judge-/Reflexions-Phase
# ─────────────────────────────────────────────────────────────────────────────
def tokenize_name(name: str) -> List[str]:
    return [t.lower() for t in re.split(r"[\s\-]+", name) if t.strip()]

def email_local(email: str) -> str:
    return email.split("@", 1)[0].lower()

def domain_of(url_or_email: str) -> str:
    if "@" in url_or_email:
        return url_or_email.split("@",1)[1].lower()
    try:
        netloc = urlparse(url_or_email).netloc.lower()
        return netloc[4:] if netloc.startswith("www.") else netloc
    except Exception:
        return ""

def proximity_scores(text: str, name: str, candidates: list[str]) -> dict:
    t = text.lower()
    pos_name = t.find(name.lower())
    scores = {}
    for c in candidates:
        pos_mail = t.find(c.lower())
        if pos_name != -1 and pos_mail != -1:
            scores[c] = abs(pos_mail - pos_name)
        else:
            scores[c] = 10_000_000
    return scores

def build_candidate_features(
        name: str,
        institution: str,
        page_url: str,
        text: str,
        candidates: list[str],
        mem_domains: dict,
        mem_patterns: dict
) -> list[dict]:
    toks = tokenize_name(name)
    last = toks[-1] if toks else ""
    initials = "".join(t[0] for t in toks) if toks else ""
    prox = proximity_scores(text, name, candidates)
    page_dom = domain_of(page_url)

    # Memory-Hints
    inst_entry = mem_domains.get(institution, {})
    inst_domains = inst_entry.get("domains", [])
    dom_entry = mem_patterns.get(page_dom, {})
    known_pats = dom_entry.get("patterns", [])

    feats = []
    for c in candidates:
        loc = email_local(c)
        mail_dom = domain_of(c)
        feats.append({
            "email": c,
            "local_contains_lastname": bool(last and last in loc),
            "local_contains_initials": bool(initials and initials in loc),
            "page_domain": page_dom,
            "email_domain": mail_dom,
            "domain_matches_page": (mail_dom.endswith(page_dom) or page_dom.endswith(mail_dom))
            if page_dom and mail_dom else False,
            "domain_matches_institution_memory": mail_dom in inst_domains if inst_domains else False,
            "approx_distance_to_name": prox.get(c, 10_000_000),
            "memory_known_patterns_for_page_domain": known_pats,
            "local_raw": loc,
        })
    return feats

# ─────────────────────────────────────────────────────────────────────────────
# Agenten: Searcher / Judge / Reflexion
# ─────────────────────────────────────────────────────────────────────────────
def gpt_propose_links_and_plan(name: str, institution: str, mem_domains: dict) -> dict:
    inst_hint = mem_domains.get(institution, {})
    system = (
        "Du bist ein Web-Research-Agent. Erzeuge einen konkreten Suchplan, "
        "inklusive 3–6 wahrscheinlichster URLs, um die persönliche E-Mail zu finden."
    )
    user = f"""
Ziel: Finde die persönliche E-Mail von
Person: {name}
Institution: {institution}

Bekannte Memory-Hints:
{json.dumps(inst_hint, ensure_ascii=False, indent=2)}

Erwarte JSON:
{{
  "phases": [
    {{"name":"directory","queries":[...] }},
    {{"name":"profile","queries":[...] }},
    {{"name":"pdf","queries":[...] }},
    {{"name":"fallback","queries":[...] }}
  ],
  "seed_urls": ["https://...", "..."]
}}
Bevorzuge institutionelle Domains.
"""
    try:
        resp = client.chat.completions.create(
            model="gpt-4",
            messages=[{"role":"system","content":system},{"role":"user","content":user}],
            temperature=0.3,
            max_tokens=350,
        )
        txt = (resp.choices[0].message.content or "").strip()
        plan = json.loads(txt)
    except Exception:
        plan = {"phases": [], "seed_urls": []}
    plan.setdefault("phases", [])
    plan.setdefault("seed_urls", [])
    return plan

def gpt_judge_once(name: str, institution: str, page_url: str, page_text: str, features: list[dict]) -> dict:
    """Ein einzelnes Urteil (ohne Self-Consistency)."""
    if not features:
        return {"chosen_email":"", "classification":"uncertain", "confidence":0.0, "reason":"No candidates"}
    context = page_text[:3500]
    system = (
        "Du bist ein akribischer E-Mail-Judge-Agent. "
        "Entscheide, ob eine E-Mail eine PERSONEN-Adresse (individuell) oder ROLLEN-Adresse ist. "
        "Wähle genau EINE E-Mail NUR, wenn sie plausibel der gesuchten Person zuzuordnen ist. "
        "Wenn offenbar nur Rollenadressen existieren und die Seite das nahelegt, darfst du eine Rollenadresse wählen. "
        "Antworte als JSON."
    )
    user = f"""
Gesuchte Person: {name}
Institution: {institution}
Seite: {page_url}

Kandidaten mit Features:
{json.dumps(features, ensure_ascii=False, indent=2)}

Textauszug (gekürzt):
---
{context}
---

Kriterien:
1) Personenadressen bevorzugen: Namens-/Initialenmatch, Profilkontext, Nähe zum Namen.
2) Domain-Plausibilität: offizielle Institut-/Uni-Domain > Drittanbieter.
3) Kontextnähe: räumliche Nähe zum Personennamen im Text.
4) Rollenadresse nur wählen, wenn ersichtlich, dass das die einzige offizielle Kontaktmöglichkeit für diese Person ist.
5) Wenn unklar: keine E-Mail.

JSON-Felder:
- chosen_email (string),
- classification ("personal" | "role" | "uncertain"),
- confidence (0..1),
- reason (kurz).
"""
    try:
        resp = client.chat.completions.create(
            model="gpt-4",
            messages=[{"role":"system","content":system},{"role":"user","content":user}],
            temperature=0.2,
            max_tokens=220,
        )
        raw = (resp.choices[0].message.content or "").strip()
        data = json.loads(raw)
    except Exception as e:
        # heuristische Rettung
        emails = RE_EMAIL.findall(locals().get("raw",""))
        data = {
            "chosen_email": sanitize_email(emails[0]) if emails else "",
            "classification": "uncertain",
            "confidence": 0.4 if emails else 0.0,
            "reason": f"Non-JSON/parse issue: {e}"
        }
    data["chosen_email"] = sanitize_email(data.get("chosen_email","") or "") or ""
    data["classification"] = data.get("classification","uncertain")
    try:
        data["confidence"] = float(data.get("confidence", 0.0) or 0.0)
    except Exception:
        data["confidence"] = 0.0
    data["reason"] = data.get("reason","")
    return data

def gpt_judge_consensus(name, institution, page_url, page_text, features, votes=JUDGE_VOTES) -> dict:
    """Self-Consistency über n Urteile; Mehrheitswahl + mittlere Confidence."""
    results = []
    for _ in range(max(1, votes)):
        r = gpt_judge_once(name, institution, page_url, page_text, features)
        if r.get("chosen_email"):
            results.append(r)
    if not results:
        return {"chosen_email":"", "classification":"uncertain", "confidence":0.0, "reason":"no consensus"}

    # Gruppiere nach Email
    from collections import defaultdict, Counter
    groups = defaultdict(list)
    for r in results:
        groups[r["chosen_email"]].append(r)
    # Gewinner: häufigste, danach höchste mittlere Confidence
    def score_group(g):
        confs = [x["confidence"] for x in g]
        return (len(g), sum(confs)/len(confs))
    winner_email, group = max(groups.items(), key=lambda kv: score_group(kv[1]))
    avg_conf = sum(x["confidence"] for x in group)/len(group)
    cls_counts = Counter([x["classification"] for x in group])
    cls = cls_counts.most_common(1)[0][0]
    reasons = list({x["reason"] for x in group if x.get("reason")})
    reason = "Consensus: " + " | ".join(reasons[:3]) if reasons else "Consensus of multiple votes."
    return {"chosen_email": winner_email, "classification": cls, "confidence": float(avg_conf), "reason": reason}

def gpt_reflect(name: str, institution: str, page_url: str, features: list[dict], verdict: dict) -> dict:
    """Reflektor-Agent prüft, ob das Urteil ausreichend begründet/sicher ist oder ob wir weitersuchen sollen."""
    system = (
        "Du bist ein kritischer Reflektor-Agent. Prüfe das Urteil eines Judge-Agents "
        "auf Plausibilität und entscheide: 'accept' oder 'continue'. Antworte als JSON."
    )
    user = f"""
Person: {name}
Institution: {institution}
Seite: {page_url}

Features (gekürzt):
{json.dumps(features[:6], ensure_ascii=False, indent=2)}

Urteil:
{json.dumps(verdict, ensure_ascii=False, indent=2)}

Entscheide:
- action: "accept" oder "continue"
- reason: kurzer Grund
"""
    try:
        resp = client.chat.completions.create(
            model="gpt-4",
            messages=[{"role":"system","content":system},{"role":"user","content":user}],
            temperature=0.1,
            max_tokens=120,
        )
        txt = (resp.choices[0].message.content or "").strip()
        data = json.loads(txt)
    except Exception:
        data = {"action":"continue","reason":"fallback: could not parse reflection"}
    if data.get("action") not in ("accept","continue"):
        data["action"] = "continue"
    data["reason"] = data.get("reason","")
    return data

# ─────────────────────────────────────────────────────────────────────────────
# Query-Baseline (Fallback, falls Plan mager ist)
# ─────────────────────────────────────────────────────────────────────────────
def build_baseline_queries(name: str, institution: str) -> List[str]:
    base = f'{name} {institution}'.strip()
    queries = [
        f"{base} contact",
        f"{base} email",
        f"{name} {institution} site:.edu",
        f"{name} {institution} site:.ac",
        f"{name} {institution} site:.org",
        f"{name} {institution} profile",
        f'"{name}" "{institution}" email',
        f'"{name}" "{institution}" filetype:pdf',
    ]
    seen, out = set(), []
    for q in queries:
        q = q.strip()
        if q and q not in seen:
            seen.add(q)
            out.append(q)
    return out

# ─────────────────────────────────────────────────────────────────────────────
# Person-Pipeline (Sense → Think → Act)
# ─────────────────────────────────────────────────────────────────────────────
def try_links_for_person(name: str, institution: str, links: List[str], used_q: str,
                         mem_domains: dict, mem_patterns: dict, t0: float) -> Optional[Tuple[str, str, str, float]]:
    pages_checked = 0
    for url in links:
        if pages_checked >= MAX_PAGES_TO_SCAN:
            break
        time.sleep(random.uniform(*SLEEP_BETWEEN_REQUESTS))
        try:
            text, html = fetch_page(url)
        except Exception as e:
            log_jsonl(RUNS_LOG, {"step":"fetch_error", "name":name, "institution":institution, "url":url, "error":str(e)})
            pages_checked += 1
            continue

        candidates = extract_emails_from_text_and_html(text, html)
        log_jsonl(RUNS_LOG, {"step":"extract", "name":name, "institution":institution, "url":url, "candidates":candidates[:10]})

        if not candidates:
            pages_checked += 1
            continue

        feats = build_candidate_features(name, institution, url, text, candidates, mem_domains, mem_patterns)
        verdict = gpt_judge_consensus(name, institution, url, text, feats, votes=JUDGE_VOTES)
        print(f"🤖 Judge: {verdict.get('classification')} | conf={verdict.get('confidence'):.2f} | {verdict.get('reason')}")
        log_jsonl(RUNS_LOG, {"step":"judge", "name":name, "institution":institution, "url":url, "verdict":verdict})

        # Annahmekriterien
        accept = False
        if verdict["chosen_email"]:
            if verdict["classification"] == "personal" and verdict["confidence"] >= ACCEPT_CONF_PERSONAL:
                accept = True
            elif verdict["classification"] == "role" and verdict["confidence"] >= ACCEPT_CONF_ROLE:
                accept = True
            elif verdict["classification"] == "uncertain" and verdict["confidence"] >= ACCEPT_CONF_UNCERTAIN:
                accept = True

        if accept:
            # Reflexions-Check
            reflect = gpt_reflect(name, institution, url, feats, verdict)
            print(f"🪞 Reflection: {reflect.get('action')} | {reflect.get('reason')}")
            log_jsonl(RUNS_LOG, {"step":"reflect", "name":name, "institution":institution, "url":url, "reflection":reflect})

            if reflect.get("action") == "accept":
                duration = time.time() - t0
                email = verdict["chosen_email"]

                # Memory updaten
                memo_domains_add(mem_domains, institution, domain_of(url))
                if email:
                    memo_patterns_add(mem_patterns, email, name)

                return email, url, used_q, duration

        pages_checked += 1

    return None

def process_person(name: str, institution: str,
                   mem_domains: dict, mem_patterns: dict) -> Tuple[str, str, str, float]:
    t0 = time.time()
    print(f"\n➡️  {name} | {institution}")

    # 1) Searcher-Agent: Plan + Seed-Links
    plan = gpt_propose_links_and_plan(name, institution, mem_domains)
    seed_links = plan.get("seed_urls", [])[:8]
    phases = plan.get("phases", [])

    # 1a) Seed-Links zuerst
    res = try_links_for_person(name, institution, seed_links, "gpt_seed", mem_domains, mem_patterns, t0)
    if res:
        return res

    # 2) Phasengetriebene Queries (der Agent hat sie vorgeschlagen)
    for phase in phases:
        phase_name = phase.get("name", "phase")
        for q in phase.get("queries", [])[:2]:
            links = ddg_search(q, MAX_SEARCH_RESULTS)
            res = try_links_for_person(name, institution, links, f"gpt_{phase_name}", mem_domains, mem_patterns, t0)
            if res:
                return res

    # 3) Fallback: Baseline-Queries
    for q in build_baseline_queries(name, institution):
        links = ddg_search(q, MAX_SEARCH_RESULTS)
        res = try_links_for_person(name, institution, links, q, mem_domains, mem_patterns, t0)
        if res:
            return res

    # Nichts gefunden
    return "", "", "", time.time() - t0

# ─────────────────────────────────────────────────────────────────────────────
# Main
# ─────────────────────────────────────────────────────────────────────────────
def main():
    people = read_two_col_csv(INPUT_CSV)

    # Memory laden
    mem_domains = load_json(MEM_DOMAINS, {})
    mem_patterns = load_json(MEM_PATTERNS, {})

    results: List[Dict[str, str]] = []
    total_found = 0
    t0_all = time.time()

    print(f"📄 Eingelesen: {len(people)} Einträge aus {INPUT_CSV}")

    for idx, (name, inst) in enumerate(people, start=1):
        email, src, used_q, dur = process_person(name, inst, mem_domains, mem_patterns)

        # Log (sichtbar): Quelle/Query/Dauer
        if email:
            total_found += 1
            print(f"✅ Gefunden: {email}")
            if src:    print(f"🔗 Quelle: {src}")
            if used_q: print(f"🔎 Query:  {used_q}")
        else:
            print("❌ Keine E-Mail gefunden.")
        print(f"⏱️ Dauer: {dur:.2f}s")

        # Ergebnis-CSV: exakt 3 Spalten
        results.append({"Name": name, "Institution": inst, "E-Mail": email})

        # Structured Log
        log_jsonl(RUNS_LOG, {
            "step":"person_done",
            "index": idx,
            "name": name,
            "institution": inst,
            "email": email,
            "source": src,
            "query": used_q,
            "duration_s": dur
        })

        # Zwischenspeicher (Memory robust halten)
        if idx % 3 == 0:
            save_json(MEM_DOMAINS, mem_domains)
            save_json(MEM_PATTERNS, mem_patterns)

    # Final: Ergebnisse speichern
    write_results_csv(results, OUTPUT_CSV)
    save_json(MEM_DOMAINS, mem_domains)
    save_json(MEM_PATTERNS, mem_patterns)

    print(f"\n✅ Gesamt: {total_found}/{len(people)} E-Mails gefunden")
    print(f"⏱️ Gesamtdauer: {time.time() - t0_all:.2f}s")
    print(f"📦 Ergebnisse gespeichert in: {OUTPUT_CSV}")
    print(f"🧠 Memory gespeichert in: {MEM_DOMAINS}, {MEM_PATTERNS}")
    print(f"🧾 Run-Log: {RUNS_LOG}")

if __name__ == "__main__":
    main()
