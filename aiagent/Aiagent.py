import os
import time
import re
import pandas as pd
from crewai import Crew, Agent, Task
from dotenv import load_dotenv
from duckduckgo_search import DDGS
from selenium import webdriver
from selenium.webdriver.chrome.options import Options
from selenium.webdriver.common.by import By
from selenium.webdriver.chrome.service import Service
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
    options.add_argument("--headless")
    service = Service(CHROMEDRIVER_PATH)
    return webdriver.Chrome(service=service, options=options)

# === SUCHE ===
def search_links(query):
    time.sleep(8)  # Pause zur Vermeidung von Rate-Limits
    with DDGS() as ddgs:
        return [r['href'] for r in ddgs.text(query, max_results=MAX_RESULTS)]

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
Welche E-Mail gehört am wahrscheinlichsten zu dieser Person? Gib nur die E-Mail-Adresse zurück.
"""
    try:
        response = client.chat.completions.create(
            model="gpt-4",
            messages=[{"role": "user", "content": prompt}],
            max_tokens=50
        )
        return response.choices[0].message.content.strip()
    except Exception as e:
        print("GPT Fehler:", e)
        return emails[0] if emails else ""

# === GPT-SUCHE NACH E-MAIL ===
def search_email_with_gpt(name, institution, context):
    print(f"🤖 GPT-Suche nach E-Mail für {name}...")
    prompt = f"""
Die folgende Person heißt und arbeitet an der Institution: {name}.
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
        return response.choices[0].message.content.strip()
    except Exception as e:
        print("GPT Fehler bei direkter Suche:", e)
        return ""

# === AGENTENKLASSEN ===
class SearchAgent:
    def run(self, name, institution):
        return search_links(f"{name} {institution}")

class VisitAgent:
    def __init__(self, driver):
        self.driver = driver

    def run(self, url):
        try:
            print(f"   🌐 Besuch: {url}")
            self.driver.get(url)
            time.sleep(2)
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
    df = pd.read_csv(INPUT_CSV, header=None, names=["Name", "Institution"])
    results = []

    search_agent = SearchAgent()
    visit_agent = VisitAgent(driver)

    for idx, row in df.iterrows():
        name = row['Name']
        institution = row['Institution'] if pd.notna(row['Institution']) else ""
        print(f"\n🔍 Suche: {name} + {institution}")
        links = search_agent.run(name, institution)

        for link in links:
            context, emails = visit_agent.run(link)
            if emails:
                selected = select_best_email(name, emails, context)
                results.append({"Name": name, "Institution": institution, "Email": selected})
                break
            else:
                gpt_found = search_email_with_gpt(name, institution, context)
                if gpt_found:
                    results.append({"Name": name, "Institution": institution, "Email": gpt_found})
                    break
        else:
            print("⚠️ Keine passende E-Mail gefunden.")
            results.append({"Name": name, "Institution": institution, "Email": ""})

    driver.quit()
    pd.DataFrame(results).to_csv(OUTPUT_CSV, index=False)
    end_time = time.time()
    duration = end_time - start_time
    print(f"\n✅ Ergebnisse gespeichert in: {OUTPUT_CSV}")
    print(f"⏱️ Verarbeitungszeit: {duration:.2f} Sekunden")

if __name__ == '__main__':
    main()
