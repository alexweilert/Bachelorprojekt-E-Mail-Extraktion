import os
import time
import re
import json
import pandas as pd
import requests
import urllib.parse
from bs4 import BeautifulSoup
from dotenv import load_dotenv
from selenium import webdriver
from selenium.webdriver.chrome.options import Options
from selenium.webdriver.common.by import By
from selenium.webdriver.chrome.service import Service
from selenium.webdriver.support.ui import WebDriverWait
from selenium.webdriver.support import expected_conditions as EC
from crewai import Crew, Agent, Task
from openai import OpenAI
from langchain_community.vectorstores import FAISS
from langchain_openai import OpenAIEmbeddings
from langchain.schema import Document

load_dotenv()
client = OpenAI()
INPUT_CSV = "list_of_names_and_affiliations.csv"
OUTPUT_CSV = "emails_ai_agent.csv"
CHROMEDRIVER_PATH = "../aiagent/chromedriver-win64/chromedriver.exe"
MEMORY_PATH = "email_memory_index"

embedding_model = OpenAIEmbeddings()

def sanitize_email(raw):
    clean = raw.strip().strip('<>"\'\t\n ').replace("mailto:", "")
    if "@" not in clean or ' ' in clean or len(clean) > 100:
        return ""
    return clean if re.match(r"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$", clean) else ""

def extract_emails(text):
    pattern = r"[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}"
    return list(set(re.findall(pattern, text)))

def initialize_memory():
    if os.path.exists(MEMORY_PATH):
        return FAISS.load_local(MEMORY_PATH, embedding_model)
    else:
        return None

def remember_email(memory, name, email):
    if memory is None:
        doc = Document(page_content=email, metadata={"name": name})
        memory = FAISS.from_documents([doc], embedding_model)
    else:
        doc = Document(page_content=email, metadata={"name": name})
        memory.add_documents([doc])
    memory.save_local(MEMORY_PATH)
    return memory

def recall_email(memory, name):
    if memory is None:
        return None
    results = memory.similarity_search(name, k=1)
    if results:
        return results[0].page_content
    return None

def name_institution_agent(entry):
    if "," in entry:
        parts = entry.split(",")
        return parts[0].strip(), ",".join(parts[1:]).strip()
    else:
        prompt = f"Zerlege folgenden Eintrag in Name + Institution als JSON: \"{entry}\""
        try:
            response = client.chat.completions.create(
                model="gpt-4",
                messages=[{"role": "user", "content": prompt}],
                max_tokens=100
            )
            raw = response.choices[0].message.content.strip()
            parsed = json.loads(raw)
            return parsed.get("name", entry), parsed.get("institution", "")
        except Exception as e:
            print("❌ Fehler bei name_institution_agent:", e)
            return entry, ""

def search_links_agent(name, institution):
    search_agent = Agent(
        role="Web-Rechercheur",
        goal="Finde reale Webseiten mit Kontaktdaten",
        backstory="Du bist spezialisiert auf akademische Webrecherche und extrahierst verlässliche URLs.",
        verbose=True
    )

    task = Task(
        description=f"Suche nach Webseiten, auf denen Kontaktinformationen zur folgenden Person zu finden sind: {name}. Gib ausschließlich gültige, öffentlich erreichbare URLs zurück. Wenn du dir unsicher bist, gib keine an.",
        expected_output="5 vollständige URLs im Format https://...",
        agent=search_agent
    )

    crew = Crew(
        agents=[search_agent],
        tasks=[task]
    )

    result = crew.kickoff()
    result_text = getattr(result, 'final_output', str(result))
    raw_links = re.findall(r"https?://[^\s\]\)]+", result_text)

    # Nur erreichbare URLs behalten
    verified_links = []
    for url in raw_links:
        try:
            response = requests.head(url, timeout=5, allow_redirects=True)
            if response.status_code < 400:
                verified_links.append(url)
        except:
            pass

    return verified_links[:5]

def visit_agent(driver, url):
    try:
        driver.get(url)
        WebDriverWait(driver, 10).until(EC.presence_of_element_located((By.TAG_NAME, "body")))
        # Text extrahieren
        text = driver.find_element(By.TAG_NAME, "body").text
        emails_text = extract_emails(text)
        # HTML-Quelltext ebenfalls scannen
        raw_html = driver.page_source
        emails_html = extract_emails(raw_html)

        all_emails = list(set(emails_text + emails_html))

        print("📄 Seite geladen:", url)
        print("🔍 E-Mails aus Text:", emails_text)
        print("🔍 E-Mails aus HTML:", emails_html)
        print("📧 Gesamt:", all_emails)
        print("📄 Textlänge:", len(text))

        return text, all_emails
    except Exception as e:
        print("❌ Fehler bei visit_agent:", e)
        return "", []

def email_verifier_agent(name, institution, candidate_email, context):
    prompt = f"""
Die folgende Person heißt und arbeitet an der Institution: {name}
Ein möglicher E-Mail-Kandidat lautet: {candidate_email}

Kontextauszug der Webseite:
---
{context[:2000]}
---

Ist diese E-Mail sehr wahrscheinlich korrekt für die Person? Antworte mit JA oder NEIN.
"""
    try:
        response = client.chat.completions.create(
            model="gpt-4",
            messages=[{"role": "user", "content": prompt}],
            max_tokens=10
        )
        result = response.choices[0].message.content.strip().lower()
        return "ja" in result
    except Exception as e:
        print("❌ Fehler bei email_verifier_agent:", e)
        return True

def email_extractor_agent(name, institution, context, emails):
    prompt = f"""
Die folgende Person heißt und arbeitet an der Institution: {name}
Im folgenden Text wurden E-Mail-Adressen gefunden: {emails}

Wähle diejenige, die am wahrscheinlichsten zu dieser Person gehört.
Bevorzuge Adressen, die Initialen der Person enthalten.
Falls du unsicher bist, gib trotzdem die wahrscheinlich passendste Adresse zurück.
Antwort nur mit einer gültigen E-Mail-Adresse, ohne weiteren Text.
---
{context[:3000]}
---
"""
    try:
        response = client.chat.completions.create(
            model="gpt-4",
            messages=[{"role": "user", "content": prompt}],
            max_tokens=50
        )
        raw_response = response.choices[0].message.content.strip()
        found = extract_emails(raw_response)
        if found:
            candidate = found[0]
            if email_verifier_agent(name, institution, candidate, context):
                return candidate
            else:
                print("⚠️ Verifizierungs-Agent hat die E-Mail abgelehnt.")
                return ""
        elif emails:
            for mail in emails:
                if any(domain in mail for domain in [".edu", ".ac.at", "@sbg.ac.at", "@bu.edu"]):
                    return mail
            return ""
        else:
            return ""
    except Exception as e:
        print("❌ Fehler bei email_extractor_agent:", e)
        return emails[0] if emails else ""

def setup_driver():
    options = Options()
    service = Service(CHROMEDRIVER_PATH)
    return webdriver.Chrome(service=service, options=options)

def main():
    total_start = time.time()
    driver = setup_driver()
    df = pd.read_csv(INPUT_CSV, header=None, names=["Full"])
    memory = initialize_memory()

    results = []
    found = 0

    for _, row in df.iterrows():
        entry = row["Full"]
        start = time.time()

        name, institution = name_institution_agent(entry)

        # 🧠 Speicherprüfung
        recalled_email = recall_email(memory, name)
        if recalled_email:
            print("🧠 Aus Memory:", recalled_email)
            results.append({
                "Name": name,
                "Institution": institution,
                "Email": recalled_email,
                "Quelle": "Memory",
                "Dauer_s": f"{time.time() - start:.2f}"
            })
            found += 1
            continue

        links = search_links_agent(name, institution)
        best_email = ""
        best_source = ""

        for link in links:
            context, emails = visit_agent(driver, link)
            email = email_extractor_agent(name, institution, context, emails)
            if email:
                best_email = sanitize_email(email)
                best_source = link
                remember_email(memory, name, best_email)
                break

        if best_email:
            found += 1

        results.append({
            "Name": name,
            "Institution": institution,
            "Email": best_email,
            "Quelle": best_source,
            "Dauer_s": f"{time.time() - start:.2f}"
        })

    driver.quit()
    duration = time.time() - total_start
    pd.DataFrame(results).to_csv(OUTPUT_CSV, index=False)
    print(f"\n✅ Gesamt: {found}/{len(df)} E-Mails gefunden")
    print(f"⏱️ Gesamtdauer: {duration:.2f} Sekunden")

if __name__ == '__main__':
    main()
