# CyberStrike framework corpora

> Source: CyberStrikeus/CyberStrike `.cyberstrike/skill/` @ `9510e3c` (imported
> 2026-11-26). Upstream generated corpora, copied verbatim as SKILL.md
> sub-skills under `references/`. They are support files: Hermes does not list
> them in the skill catalog, so load one explicitly with
> `skill_view(name='cyberstrike', file_path='references/...')`, or search them
> on disk with `rg`.
>
> ARTIFEX mounts the same tree at `/app/skills/cyberstrike/references/` — agents
> read a note with the `Read` tool or `rg`/`cat` via `Bash`.

| Corpus | SKILL.md | Layout | Example |
| --- | --- | --- | --- |
| CIS benchmarks | 5,000 | `references/CIS_benchmarks/<family>/<vendor>/<benchmark>/<control>/SKILL.md` | `references/CIS_benchmarks/Cloud_Providers/AWS/cis-amazon-web-services-foundations/cis-aws-foundations-2.1.1/SKILL.md` |
| NIST | 1,606 | `references/NIST/<framework>/<family>/<control>/SKILL.md` | `references/NIST/CSF_v2.0/DE_detect/DE.AE-01_deae-01/SKILL.md` |
| MITRE ATT&CK Enterprise | 691 | `references/mitre_attack/<TA tactic>/<technique>/SKILL.md` | `references/mitre_attack/TA0001_initial-access/T1566_phishing/SKILL.md` |
| MITRE ATT&CK ICS | 83 | `references/mitre_attack_ics/<TA tactic>/<technique>/SKILL.md` | — |
| MITRE ATT&CK Mobile | 124 | `references/mitre_attack_mobile/<TA tactic>/<technique>/SKILL.md` | — |
| OWASP WSTG 4.2 | 125 | `references/WEB/OWASP_WSTG_4.2/<wstg-id>/SKILL.md` | `references/WEB/OWASP_WSTG_4.2/wstg-apit-00/SKILL.md` |

## Coverage

- **CIS** — 61 benchmarks in 3 families:
  - `Cloud_Providers/{AWS, Microsoft_Azure, Google_Cloud_Platform, Google_Workspace}`
  - `Operating_Systems/Ubuntu`
  - `Server_Software/{Apache_Cassandra, Apache_HTTP_Server, Apache_Tomcat, Bind, Docker, Kubernetes, Nginx}`
- **NIST** — CSF v2.0 (GV/ID/PR/DE/RS/RC), SP800-53 rev5 (AC/AT/AU/CA/CM/CP/IA/IR/MA/MP/PE/PL/PM/PS/PT/RA/SA/SC/SI/SR), SP800-171 rev3 (17 requirement families), SP800-218 SSDF (PO/PS/PW/RV).
- **MITRE ATT&CK** — enterprise techniques grouped by all 15 tactics, plus 12 ICS and 12 mobile tactic groups.
- **WSTG** — all 125 OWASP Web Security Testing Guide v4.2 test IDs (`wstg-apit-*`, `wstg-athn-*`, `wstg-authz-*`, `wstg-busl-*`, `wstg-clnt-*`, `wstg-conf-*`, `wstg-crypt-*`, `wstg-erro-*`, `wstg-idnt-*`, `wstg-info-*`, `wstg-inpv-*`, `wstg-sess-*`).

## Search then load

```bash
# find the note that covers a topic
rg -l 'unkeyed header' ~/.hermes/skills/security/cyberstrike/references/CIS_benchmarks
rg -l 'DE.AE-01'      ~/.hermes/skills/security/cyberstrike/references/NIST
rg -l 'T1566'         ~/.hermes/skills/security/cyberstrike/references/mitre_attack
rg -l 'inpv-05'       ~/.hermes/skills/security/cyberstrike/references/WEB
```

Then load it — Hermes: `skill_view(name='cyberstrike', file_path='<path relative to the skill dir>')`;
ARTIFEX: `Read /app/skills/cyberstrike/references/...` (the `rg` roots above
become `/app/skills/cyberstrike/references/<corpus>`).

The 33 curated playbooks are separate: flat `references/<name>.md` files, indexed
in the main SKILL.md — those are the ones worth reading before a test; the
corpora are for looking up a specific control, technique, or WSTG test.
