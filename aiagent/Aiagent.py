import pandas as pd
import re
import asyncio
from playwright.async_api import async_playwright

# Load your data
df = pd.read_csv("list_of_names_and_affiliations.csv")

def split_name_institution(entry):
    tokens = entry.split()
    for i in range(2, 5):
        name = ' '.join(tokens[:i])
        institution = ' '.join(tokens[i:])
        if institution:
            return pd.Series([name, institution])
    return pd.Series([entry, ""])

df[['Name', 'Institution']] = df.iloc[:, 0].apply(split_name_institution)

async def fetch_emails(name, institution, playwright):
    browser = await playwright.chromium.launch(headless=True)
    context = await browser.new_context()
    page = await context.new_page()

    query = f"{name} {institution}"
    await page.goto("https://duckduckgo.com")
    await page.fill("input[name='q']", query)
    await page.keyboard.press("Enter")
    await page.wait_for_selector("#links")

    links = await page.query_selector_all("#links .result__title a")
    if links:
        url = await links[0].get_attribute("href")
        await page.goto(url)
        await page.wait_for_load_state("load")
        content = await page.content()

        # Extract all email-like patterns
        emails = re.findall(r"[\w\.-]+@[\w\.-]+", content)
        # Heuristic filter: check if parts of name are in email
        name_parts = name.lower().split()
        matched_emails = [email for email in emails if any(part in email.lower() for part in name_parts)]

        await context.close()
        return name, institution, url, ", ".join(set(matched_emails))
    else:
        await context.close()
        return name, institution, "No link found", ""

async def main():
    results = []
    async with async_playwright() as playwright:
        for _, row in df.iterrows():
            name, inst = row['Name'], row['Institution']
            try:
                result = await fetch_emails(name, inst, playwright)
                print(result)
                results.append(result)
            except Exception as e:
                print(f"Error for {name}: {e}")
                results.append((name, inst, "Error", ""))

    result_df = pd.DataFrame(results, columns=["Name", "Institution", "URL", "Matched Emails"])
    result_df.to_csv("email_extraction_results.csv", index=False)

if __name__ == "__main__":
    asyncio.run(main())
