import streamlit as st
import pandas as pd
import subprocess
import os

st.set_page_config(page_title="AI Email Finder", layout="wide")
st.title("📧 AI-gesteuerte E-Mail-Suche")

uploaded_file = st.file_uploader(
    "Lade eine CSV (ohne Header) mit zwei Spalten hoch: Name | Institution",
    type=["csv"]
)

if uploaded_file:
    # ⬇️ zweispaltig einlesen und genau so speichern
    df = pd.read_csv(uploaded_file, header=None, names=["Name", "Institution"])
    df.to_csv("list_of_names_and_affiliations.csv", index=False, header=False)
    st.success("✅ Datei gespeichert: list_of_names_and_affiliations.csv (zweispaltig)")

    if st.button("🔍 Starte AI-Agentensuche"):
        with st.spinner("Agent läuft…"):
            result = subprocess.run(["python", "main.py"], capture_output=True, text=True)
            st.text(result.stdout)
            if result.stderr:
                st.error(result.stderr)

if os.path.exists("emails_ai_agent.csv"):
    st.subheader("📄 Ergebnisse")
    output = pd.read_csv("emails_ai_agent.csv")
    st.dataframe(output, use_container_width=True)
    st.download_button(
        label="📥 Ergebnisse herunterladen",
        data=output.to_csv(index=False),
        file_name="emails_ai_agent.csv",
        mime="text/csv"
    )
