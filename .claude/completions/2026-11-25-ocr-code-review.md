# 2026-11-25 — Install open-code-review skill & full codebase review

- Skill `open-code-review` dan `open-code-review-delegate` disalin dari alibaba/open-code-review ke `.claude/skills/`; CLI `ocr` v1.14.3 dipasang global via npm.
- Review seluruh codebase memakai rule OCR (delegation mode) → 20 task di `.scratch/code-review-2026-11-25/` (7 HIGH, 10 MEDIUM, 3 LOW).
- Catatan: delegation mode OCR berbasis diff; untuk full-scan rule diambil via `ocr delegate rule` lalu direview manual per area. `ocr scan` (native) butuh provider LLM sendiri.
