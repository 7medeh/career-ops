# Mode: tracker — Application Tracker

Reads and displays `data/applications.md`.

**Tracker format:**
```markdown
| # | Date | Company | Role | Score | Status | PDF | Report |
```

Canonical statuses (source of truth: `templates/states.yml`): `Evaluated` → `Applied` → `Responded` → `Interview` → `Offer` / `Rejected` / `Discarded` / `SKIP`

- `Evaluated` = report completed, decision pending
- `Applied` = the candidate submitted their application
- `Responded` = a recruiter or company made contact and the candidate replied (inbound)
- `Interview` = active interview process
- `Offer` = offer received
- `Rejected` = rejected by the company
- `Discarded` = discarded by the candidate, or the posting closed
- `SKIP` = doesn't fit, don't apply

If the user asks to update a status, edit the corresponding row.

Also show statistics:
- Total applications
- Breakdown by status
- Average score
- % with a generated PDF
- % with a generated report
