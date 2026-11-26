---
name: cyberstrike
description: "Use when you need CyberStrike's upstream security playbooks."
version: 1.0.0
metadata:
  hermes:
    tags: [security, reference, cyberstrike, web, cloud, redteam]
---

# CyberStrike reference bundle

Upstream snapshot of CyberStrike's curated skill library —
[github.com/CyberStrikeus/CyberStrike](https://github.com/CyberStrikeus/CyberStrike),
`.cyberstrike/skill/`, commit `9510e3c` (imported 2026-11-26).

33 curated playbooks plus 7,629 framework-corpus sub-skills (CIS, NIST, MITRE
ATT&CK, OWASP WSTG), body-verbatim under `references/`. None of them appears in
the skill catalog — nothing costs context until loaded.

```
# Hermes
skill_view(name='cyberstrike', file_path='references/<note>.md')
# ARTIFEX agent container (Read/Bash tools)
/app/skills/cyberstrike/references/<note>.md
```

## Curated playbooks

Several of these notes are also fused verbatim into existing skills
(web-vulnerability-classes, attacking-active-directory, exploiting-cloud-platforms,
kubernetes-attack, ci-cd-oidc-abuse, escalating-*-privileges, llm-testing, recon,
testing-apis) — use those skills for methodology; open this bundle when you want
the full upstream playbook text (notably the ones with no local home yet:
attack-ssrf, attack-ssti, attack-xxe, attack-subdomain-takeover, macos-postexploit,
bun-file-io).

## Framework corpora

The rest of the upstream library is mirrored under `references/` as SKILL.md
sub-skills — CIS, NIST, MITRE ATT&CK and OWASP WSTG. Layout, counts, example
paths and search recipes: [references/CORPORA.md](references/CORPORA.md).

- **CIS** — 5,000 controls across 61 benchmarks (AWS, Azure, GCP, Google Workspace; Ubuntu; Docker, Kubernetes, Nginx, Apache HTTP Server/Tomcat/Cassandra, Bind)
- **NIST** — 1,606 controls (CSF v2.0, SP800-53 r5, SP800-171 r3, SP800-218 SSDF)
- **MITRE ATT&CK** — 898 technique notes (691 enterprise by tactic, 83 ICS, 124 mobile)
- **OWASP WSTG 4.2** — 125 test cases

Search on disk (`rg -l '<topic>' <skill-root>/references/<corpus>` — Hermes root
`~/.hermes/skills/security/cyberstrike`, ARTIFEX root `/app/skills/cyberstrike`),
then load the hit (`skill_view(...)` in Hermes, `Read` in ARTIFEX).

## Index

| Note | Upstream description |
| --- | --- |
| `ad-security` | Active Directory security testing and attack techniques |
| `attack-cache-poison` | Web cache poisoning — unkeyed header/parameter injection to serve malicious content to all users |
| `attack-cors` | CORS misconfiguration testing — origin reflection, wildcard bypass, null origin, credential leakage |
| `attack-graphql` | GraphQL vulnerability testing — introspection exposure, complexity DoS, batch abuse, mutation auth bypass |
| `attack-host-header` | Host header injection — password reset poisoning, cache poisoning, routing bypass, SSRF via Host |
| `attack-idor-automation` | IDOR automated testing — cross-account access, horizontal/vertical privilege escalation, mass data exposure |
| `attack-jwt` | JWT token attacks — alg:none bypass, key confusion, claim tampering, signature stripping |
| `attack-open-redirect` | Open redirect exploitation — URL parameter manipulation, OAuth token theft, phishing chains |
| `attack-prototype-pollution` | JavaScript prototype pollution — __proto__ injection, constructor.prototype, gadget chain exploitation |
| `attack-race-condition` | Race condition / TOCTOU testing — concurrent requests to exploit time-of-check-to-time-of-use flaws |
| `attack-rate-limit-bypass` | Rate limit bypass testing — XFF rotation, case variation, method switching, header manipulation |
| `attack-request-smuggling` | HTTP request smuggling — CL.TE, TE.CL, TE.TE desync attacks for cache poisoning and auth bypass |
| `attack-ssrf` | Server-Side Request Forgery — internal network access, cloud metadata theft, filter bypass techniques |
| `attack-ssti` | Server-Side Template Injection — detection, engine fingerprinting, and exploitation across 7 template engines |
| `attack-subdomain-takeover` | Subdomain takeover — CNAME detection, cloud service fingerprinting, dangling DNS exploitation |
| `attack-websocket` | WebSocket security testing — CSWSH, message injection, auth bypass, origin validation |
| `attack-xxe` | XML External Entity injection — file read, SSRF, data exfiltration via out-of-band XML parsing |
| `aws-postexploit` | AWS post-exploitation — 92 programs for full kill chain from recon to cleanup via AWS CLI |
| `azure-postexploit` | Azure/Entra ID post-exploitation — 154 programs for tenant compromise, CIS compliance, identity attacks, data exfiltration, and M365 abuse |
| `bun-file-io` | Use this when you are working on file operations like reading, writing, scanning, or deleting files. It summarizes the preferred file APIs and patterns used … |
| `ci-assessment` | READ-ONLY CI/CD pipeline security assessment for GitHub Actions, dependency security, and software supply chain |
| `cicd-attacks` | CI/CD pipeline attacks for secret extraction, pipeline injection, and supply chain compromise via GitHub/Jenkins/GitLab |
| `cloud-assessment` | Multi-cloud READ-ONLY security assessment methodology for AWS, Azure, and GCP using CIS benchmark-aligned checks |
| `ebpf-attacks` | eBPF-based post-exploitation for kernel-level credential harvesting, process hiding, and traffic interception on Linux |
| `gcp-postexploit` | GCP post-exploitation for IAM privilege escalation, data exfiltration, persistence, and operational security via google-cloud SDK |
| `k8s-assessment` | READ-ONLY Kubernetes security assessment based on CIS Kubernetes Benchmark using kubectl |
| `k8s-postexploit` | Kubernetes post-exploitation for container escape, secret extraction, RBAC abuse, and cluster persistence |
| `kerberos-attacks` | Kerberos protocol attack techniques and exploitation |
| `linux-postexploit` | Linux post-exploitation — credential harvesting, privilege escalation, persistence, lateral movement, evasion, exfiltration, and network attacks with multi-e… |
| `llm-security` | OWASP LLM Top 10 security testing - prompt injection, system prompt leakage, excessive agency, sensitive data disclosure |
| `macos-postexploit` | macOS post-exploitation — 46 programs across recon, credential harvesting, privilege escalation, persistence, evasion, monitoring, lateral movement, and exfi… |
| `recon-methodology` | Bug bounty and pentest reconnaissance methodology |
| `windows-postexploit` | Windows post-exploitation — Active Directory attacks, Kerberos exploitation, ADCS abuse, lateral movement, persistence, privilege escalation, credential harv… |
