# ARTEX product

<!-- impeccable:product-schema 1 -->

## Platform
web

## Stack
The application uses a Go backend, embedded Next.js frontend and PostgreSQL. The approved separate GitHub Pages landing page uses plain HTML, CSS and JavaScript; it must run without the application backend.

## Users
Developers and security researchers evaluating AI-assisted workflows for locally isolated research and validation. This audience and the landing-page scope were approved by the owner.

## Product Purpose
Coordinate goal planning, agents, asset scope, coverage, recorded findings and human review in one self-hosted research console. The landing page helps visitors understand the product, inspect honest evidence and reach download/setup documentation.

## Positioning
Shared asset and per-task exploration graphs link targets to agent directions, observations and evidence. An external agent-control adapter exposes task and findings workflows to Claude Code, Codex and Pi.

## Operating Context
Self-hosted application requiring PostgreSQL and a separately configured model provider for agent execution. GitHub Pages hosts a public static introduction, not a live security-testing service or backend.

## Capabilities and Constraints
Features and setup are grounded in README.md, adapters/agent/README.md and code. Release v0.1.0 provides five platform archives and a multi-architecture Docker image. The adapter is a control interface, not a replacement for internal agents and not a coding-subscription-to-API-key conversion. Repository usage restrictions allow locally isolated research/validation and prohibit online target testing. One inherited backend scope-overview test remains failing; do not claim complete test success or production readiness.

## Brand Commitments
Preserve the ARTEX name and existing scope-grid logo. The application screenshots establish a precise, neutral console identity with restrained blue accents. English is the default. Respect ARTEX authorship and AGPL-3.0.

## Evidence on Hand
Repository-owned screenshots under screenshots/ show fictional demo data and must be labeled as demo screenshots. Existing logo: web/public/artex.svg. README, provenance, verification notes, adapter documentation and release artifacts are primary evidence. A real owner-run conversation was inspected in the signed-in local UI. It records task creation, inspection of the exploration graph and worker traces, and progress updates. The public page may use this privacy-safe workflow summary only; no target names, raw messages, vulnerability claims, token counts or private screenshots. This is evidence of orchestration, not independent security validation. No verified customer testimonials, adoption statistics, performance benchmarks or externally audited security claims are supplied.

## Product Principles
Keep product claims checkable. Distinguish public introduction, demo fixtures and actual runtime. Put setup and model requirements near the entry path. Preserve scope/approval mechanisms. Never publish credentials, target details or private conversations.

## Accessibility & Inclusion
Responsive desktop and mobile web, keyboard-operable controls, readable contrast, reduced-motion support and complete English copy.
