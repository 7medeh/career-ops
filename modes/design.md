# Mode: design — CV Template Selection & Design Intelligence

## When to invoke

- Any time a PDF is being generated (auto-select template unless user overrides)
- User asks "which template should I use?", "change my theme", or "show me the templates"
- User asks about CV design, visual presentation, or layout choices

---

## Template library

| ID | File | Palette | Best for |
|----|------|---------|----------|
| `tech` | `cv-template.html` | Teal + purple gradient | Engineering, AI/ML, full-stack, platform |
| `minimal` | `cv-template-minimal.html` | Monochrome navy | Consulting, finance, gov, corporate, management |
| `modern` | `cv-template-modern.html` | Bold indigo accent | Product, growth, startups, design-adjacent |

---

## Selection logic

**Step 1 — Check for a hard preference:**
Read `config/profile.yml` → `design.preferred_theme`.
- If set to `tech`, `minimal`, or `modern`: use it, skip the rest.
- If `null` or missing: proceed to signal detection.

**Step 2 — Detect from JD signals:**

Score each theme +1 per matching signal. Pick the highest score. Tie → use `tech`.

### `tech` signals
- Title contains: engineer, developer, architect, SRE, DevOps, platform, AI, ML, data
- Company type: tech startup, SaaS, cloud provider, AI lab, open source org
- Stack mentioned: React, Python, AWS, Kubernetes, LLM, API, backend, full stack
- Seniority: IC role (no direct reports mentioned)

### `minimal` signals
- Title contains: consultant, analyst, manager, director, VP, principal, associate
- Company type: Big 4 / consulting, financial services, insurance, federal agency, healthcare system, Fortune 500 non-tech
- Culture signals: formal, structured, hierarchy, compliance, regulated industry
- Seniority: leadership or management track

### `modern` signals
- Title contains: product, growth, design, UX, creative, head of, founding
- Company type: consumer tech, fintech, series A-C startup, design studio, marketplace
- Tone: mission-driven, fast-paced, values-driven, bold, ambitious
- Seniority: any

**Step 3 — Announce the selection:**
Tell the user which template was chosen and why, in one line:
> "Using **modern** template — indigo accent, left-border section headers. Good fit for a product-focused startup role. Override with `/career-ops pdf --theme=minimal` if you prefer."

---

## Design principles — what makes a resume work

### The 6-second scan
A recruiter's eye moves: name → title/headline → company names → dates. The layout must make this path frictionless.

- Name must be the largest element on the page (28-32px minimum)
- First third of the page = summary + competencies = your keyword and signal zone
- Section headers need strong visual contrast (weight + color, not just underline)
- Company names must stand out from role titles — use color or weight differentiation
- Bullet points: max 2 lines each, lead with action verb + metric or outcome

### White space is signal, not waste
Dense text signals junior. Generous spacing signals confidence and seniority. Never reduce font size below 10px or margins below 0.5in to fit more content — cut content instead.

### Color rules
- Max 2 accent colors (primary for headers/links, secondary for company names)
- Body text: never pure black — use #1a1a2e or #0f172a for rendering quality on screens and print
- Avoid: red (aggressive), orange (unprofessional), light gray body text (accessibility failure)
- For conservative industries: monochrome is a feature, not a limitation

### Typography rules
- Never mix more than 2 font families
- Heading font: geometric sans (Space Grotesk) — high distinctiveness at small sizes
- Body font: humanist sans (DM Sans) — optimized for paragraph legibility
- Size floor: 10px body, 11.5px section headers, 28px name
- Line height: 1.5-1.6 body, 1.1-1.2 headings

### ATS safety — all three templates are safe
- Single-column layout (no sidebars, no two-column grids)
- No tables, no SVG text, no text in images
- No critical info in PDF headers/footers (ATS ignores them)
- All text is UTF-8 selectable
- Font weights embedded via woff2 (Playwright resolves file:// paths)

### Industry-specific considerations
- **Tech / AI roles**: color is expected and signals you know design. Tech template or modern.
- **Consulting / Finance / Gov**: color reads as informal. Minimal template. Stick to weight and spacing as the only design lever.
- **Product / Startup**: bold single accent communicates confidence without being flashy. Modern template.
- **Relocating or applying internationally**: A4 format + neutral palette reduces culture-specific risk.

---

## How to override

Users can override template selection at any time:
- In `config/profile.yml`: set `design.preferred_theme: minimal` (or `tech` / `modern`)
- At generation time: just tell Claude "use the minimal template for this one"
- Per-company: "always use minimal for Deloitte-adjacent companies" → Claude applies the rule from context without needing a config change

---

## When to surface design choices to the user

Always auto-select silently and announce the choice. Only ask the user to choose if:
- The JD signals are genuinely ambiguous (equal score across themes)
- The user explicitly asks to see the options

If showing options, describe each in one sentence of visual language, not just a name:
- **tech**: teal-to-purple gradient accent, dual-color hierarchy — confident, technical
- **minimal**: monochrome navy, high letter-spacing, no color pop — authoritative, conservative
- **modern**: bold indigo accent line, left-border section markers — sharp, product-minded
