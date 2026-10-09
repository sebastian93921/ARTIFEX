# ARTIFEX provenance

This tree is [Autumn-27/ARTEX](https://github.com/Autumn-27/ARTEX) based on commit `160fe13`,
rebranded as **ARTIFEX**. An intermediate derivative (ScopeWeaver) had localized and renamed the
product; that rebrand was reverted to the upstream naming first, and this tree now applies its own
ARTIFEX naming on top. Beyond naming, the removal of the Korean localization (English-only
interface) and the optional API key for local-model base URLs, nothing else about the code changed.

The `LICENSE` is retained unchanged. Original authorship, contributor acknowledgments and upstream
changelog history remain attributed to ARTEX by Autumn-27.

## Naming contract in this tree

- Go module `github.com/sebastian93921/artifex`, source entry point `./cmd/artifex`.
- `ARTIFEX_*` configuration/env keys (upstream used `ARTEX_*`), e.g. `ARTIFEX_PG_DSN`,
  `ARTIFEX_LANGUAGE`, `ARTIFEX_TARGET_OS`.
- `artifex_locale` cookie (upstream: `artex_locale`).
- `artifex` database name/role defaults in Docker Compose; `ARTIFEX` login username default for
  fresh installs (existing databases keep their configured user).
- Executable `artifex` (`artifex.exe` on Windows); archives `artifex-<version>-<os>-<arch>.zip`.

## Update provenance

`selfupdate` resolves releases from this fork's repository (`sebastian93921/ARTIFEX`) rather than
from upstream; upstream remains credited above and in [docs/VERIFICATION.md](VERIFICATION.md).
