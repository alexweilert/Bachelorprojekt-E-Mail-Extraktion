#!/bin/bash

# Setup-Skript für Bachelorprojekt: AI-Agentensystem mit Streamlit und CrewAI

echo "🔧 Erstelle virtuelle Umgebung (.venv)"
python -m venv .venv

source .venv/bin/activate || source .venv/Scripts/activate

echo "⬇️ Installiere notwendige Pakete"
pip install --upgrade pip
pip install streamlit selenium openai pandas python-dotenv langchain langchain-community langchain-openai faiss-cpu

echo "✅ Installation abgeschlossen"
echo "💡 Starte die Anwendung mit: streamlit run streamlit_interface.py"
