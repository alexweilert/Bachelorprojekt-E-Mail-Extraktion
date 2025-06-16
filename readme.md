Gegeben: eine Liste von Namen + Institution als CSV-File (ein Beispiel ist im Anhang)

zu implementieren:
(1) konventionelle Verarbeitung: es wird das DuckDuckGo-API für die Suchabfrage verwendet (Suche jeweils nach Name+Institution)
=> die Links werden aus der Abfrage gesucht; pro Link: Suche nach einer E-Mail-Adresse, die möglichst dem Namen entspricht
Output: ein CSV-File mit einer Spalte Namen + Institution und einer Spalte E-Mail-Adresse

(2) Verarbeitung mit Llama: es wird das DuckDuckGo-API für die Suchabfrage verwendet (Suche jeweils nach Name+Institution)
=> die Links werden aus der Abfrage mit Llama gesucht; pro Link: Llama -Suche nach einer E-Mail-Adresse,
die möglichst dem Namen entspricht
Output: ein CSV-File mit einer Spalte Namen + Institution und einer Spalte E-Mail-Adresse

(3) Verarbeitung mit AI Agents, die eine UI-Oberfläche bedienen können (siehe Text Training for Computer Use anbei)

Vergleich der Qualität der Ergebnisse
Vergleich der Implementierungen: # Lines of Code, was konnte generiert werden, was nicht; Code-Komplexität
Vergleich der Kosten für die LLM-Verwendung bei (2) und (3)

Ich würde es bevorzugen, dass Sie die Varianten (insbesondere 1 und 2) in Go implementieren,
auch weil ich annehme, dass Sie Go nicht gut kennen und es interessant ist, wieviel Aufwand die Einarbeitung bedeutet,
wenn man GPT/Copilot für das Code-Generieren verwendet 