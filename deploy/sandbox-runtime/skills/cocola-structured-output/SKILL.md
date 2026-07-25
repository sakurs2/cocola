---
name: cocola-structured-output
description: Use Cocola's optional structured presentation tools when a summary card, comparison table, item list, or metric grid would make the final answer materially easier to scan. Trigger for clearly structured results; do not use it for ordinary explanations, code, plans, error messages, or short prose answers.
---

# Cocola Structured Output

Cocola may expose optional presentation tools during Execute runs. Use at most
one, after all research and other tool work is complete:

- `cocola_present_summary`: a compact set of labeled facts with a short summary.
- `cocola_present_table`: comparable records with explicit columns and rows.
- `cocola_present_list`: a homogeneous collection of items.
- `cocola_present_metrics`: up to 20 labeled measurements.

Choose a presentation tool only when its structure is clearer than a normal
Markdown answer. Do not force narrative content into a card. If no presentation
tool is available or none fits the result, answer normally.

The presentation tool is terminal for the current Run. Its typed input schema is
the authority for the accepted fields and limits; do not invent HTML, Markdown
tags, or a fallback output protocol.
