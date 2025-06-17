import os
import time
import re
import pandas as pd
from crewai import Crew, Agent, Task
from dotenv import load_dotenv
from selenium import webdriver
from selenium.webdriver.chrome.options import Options
from selenium.webdriver.common.by import By
from selenium.webdriver.chrome.service import Service
from selenium.webdriver.support.ui import WebDriverWait
from selenium.webdriver.support import expected_conditions as EC
from openai import OpenAI

# === SETUP ===
load_dotenv()
client = OpenAI()
INPUT_CSV = "list_of_names_and_affiliations.csv"
OUTPUT_CSV = "emails_ai_agent.csv"
CHROMEDRIVER_PATH = "chromedriver-win64/chromedriver.exe"
MAX_RESULTS = 3

# === CHROME DRIVER SETUP ===
def setup_driver():
    options = Options()
    # options.add_argument("--headless")  # Entfernt für vollständiges Rendern
    service = Service(CHROMEDRIVER_PATH)
    return webdriver.Chrome(service=service, options=options)

# === GPT-SUCHE NACH LINKS UND EXTRAKTION VON NAME/INSTITUTION ===
def search_links_with_gpt(full_entry):
    print(f"🤖 GPT-Linksuche für {full_entry}...")

    # Zerlege Zeile lokal in Name + Institution, wenn möglich
    if "," in full_entry:
        parts = full_entry.split(",")
        name = parts[0].strip()
        institution = ",".join(parts[1:]).strip()
    else:
        # Fallback auf GPT-Extraktion, falls kein Komma vorhanden
        extraction_prompt = f"""
Die folgende Eingabe enthält den vollständigen Namen und ggf. die Institution einer Person:
"{full_entry}"

Zerlege die Eingabe in zwei Teile:
1. Den vollständigen Namen der Person
2. Die Institution, falls vorhanden

Antwortformat (JSON):
{{"name": "...", "institution": "..."}}
"""
        try:
            extraction_response = client.chat.completions.create(
                model="gpt-4",
                messages=[{"role": "user", "content": extraction_prompt}],
                max_tokens=100
            )
            extracted = extraction_response.choices[0].message.content.strip()
            print("GPT-Extraktion:", extracted)
            import json
            extracted_data = json.loads(extracted)
            name = extracted_data.get("name", full_entry).strip()
            institution = extracted_data.get("institution", "").strip()
        except Exception as e:
            print("GPT Fehler bei Name/Institution-Extraktion:", e)
            name = full_entry.strip()
            institution = ""

    search_prompt = f"""
Du bist ein Recherche-Assistent. Suche im Web nach der besten Webseite, um Kontaktinformationen zu finden für:

Name: {name}
Institution: {institution}

Gib mir eine Liste von 1–3 Links zurück, auf denen sehr wahrscheinlich eine E-Mail steht. Antworte nur mit URLs, eine pro Zeile.
"""
    try:
        response = client.chat.completions.create(
            model="gpt-4",
            messages=[{"role": "user", "content": search_prompt}],
            max_tokens=300
        )
        links = [line.strip() for line in response.choices[0].message.content.strip().splitlines() if line.strip().startswith("http")]
        return name, institution, links
    except Exception as e:
        print("GPT Fehler bei Link-Suche:", e)
        return name, institution, []

# === E-MAIL EXTRAKTION ===
def extract_emails(text):
    pattern = r"[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}"
    return list(set(re.findall(pattern, text)))

# === GPT-ENTSCHEIDUNG ===
def select_best_email(name, emails, context):
    if not emails:
        print("⚠️ Keine E-Mails zum Auswerten durch GPT")
        return ""
    print(f"🤖 GPT-Auswahl unter {len(emails)} E-Mail(s) für {name}...")
    prompt = f"""
Die folgende Person heißt: {name}
Gefundene E-Mails: {emails}
Kontext der Webseite:
{context[:3000]}

Welche dieser E-Mail-Adressen gehört am wahrscheinlichsten zu der Person?
Falls du dir nicht sicher bist, gib die passendste zurück.
Antworte nur mit der E-Mail-Adresse.
"""
    try:
        response = client.chat.completions.create(
            model="gpt-4",
            messages=[{"role": "user", "content": prompt}],
            max_tokens=50
        )
        answer = response.choices[0].message.content.strip()
        print("GPT-Rohantwort:", answer)
        valid_emails = extract_emails(answer)
        return valid_emails[0] if valid_emails else emails[0]  # fallback
    except Exception as e:
        print("GPT Fehler:", e)
        return emails[0] if emails else ""

# === GPT-SUCHE NACH E-MAIL ===
def search_email_with_gpt(name, institution, context):
    print(f"🤖 GPT-Suche nach E-Mail für {name}...")
    prompt = f"""
Die folgende Person heißt: {name}, sie arbeitet an der Institution: {institution}.
Basierend auf dem folgenden Webseiteninhalt, finde die passende E-Mail-Adresse, wenn sie enthalten ist.
Gib nur eine E-Mail zurück oder "" falls keine gefunden wurde.

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
        answer = response.choices[0].message.content.strip()
        print("GPT-Rohantwort:", answer)
        valid_emails = extract_emails(answer)
        return valid_emails[0] if valid_emails else ""
    except Exception as e:
        print("GPT Fehler bei direkter Suche:", e)
        return ""

# === AGENTENKLASSEN ===
class SearchAgent:
    def run(self, full_entry):
        return search_links_with_gpt(full_entry)

class VisitAgent:
    def __init__(self, driver):
        self.driver = driver

    def run(self, url):
        try:
            print(f"   🌐 Besuch: {url}")
            self.driver.get(url)
            WebDriverWait(self.driver, 10).until(
                EC.presence_of_element_located((By.TAG_NAME, "body"))
            )
            text = self.driver.find_element(By.TAG_NAME, "body").text
            emails = extract_emails(text)
            print(f"   ✉️ Gefundene E-Mails: {emails if emails else 'Keine'}")
            return text, emails
        except Exception as e:
            print(f"   ⚠️ Fehler beim Besuch der Seite: {e}")
            return "", []

# === MAIN ===
def main():
    start_time = time.time()
    driver = setup_driver()

    raw_df = pd.read_csv(INPUT_CSV, header=None, names=["Name"])
    results = []

    search_agent = SearchAgent()
    visit_agent = VisitAgent(driver)

    for idx, row in raw_df.iterrows():
        full_entry = row.iloc[0]
        name, institution, links = search_agent.run(full_entry)

        print(f"\n🔍 Suche: {name} + {institution}")
        found_email = ""

        for link in links:
            context, emails = visit_agent.run(link)

            if emails:
                selected = select_best_email(name, emails, context)
                if selected:
                    found_email = selected
                    break

            gpt_found = search_email_with_gpt(name, institution, context)
            if gpt_found:
                found_email = gpt_found
                break

        results.append({"Name": name, "Institution": institution, "Email": found_email})

    driver.quit()
    pd.DataFrame(results).to_csv(OUTPUT_CSV, index=False)
    end_time = time.time()
    duration = end_time - start_time
    print(f"\n✅ Ergebnisse gespeichert in: {OUTPUT_CSV}")
    print(f"⏱️ Verarbeitungszeit: {duration:.2f} Sekunden")

if __name__ == '__main__':
    main()
