# Changelog

All notable changes are documented here in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format.

## [0.20.0] - 2026-11-11

### Added

- Session stickiness, channel editing, group/allowance deletion, Codex images, OpenCode Go usage and proxy IP reports (#160)

## [0.19.2] - 2026-11-05

### Fixed

- Distinguish Codex Team members

## [0.19.1] - 2026-11-01

### Fixed

- Allow adding rules over HTTP (#155)

## [0.19.0] - 2026-11-01

### Added

- Add owner-controlled web updates (#152)

### Fixed

- Use instance calendar days for reports (#151)

## [0.18.1] - 2026-11-01

### Fixed

- Clarify allocation card layout (#148)
- Aggregate hourly activity in the instance time zone (#149)

## [0.18.0] - 2026-11-01

### Added

- Support resource group rebinding and recovery (#146)

### Fixed

- Omit hidden models from price warnings (#145)

## [0.17.0] - 2026-10-30

### Added

- Add platform presets and searchable setup (#142)
- Add model capabilities and usage insights (#143)

### Changed

- **BREAKING:** Add shared budgets across duration windows (#141) — Existing allocation configuration writes require both expected revision fields; future writes also require confirmed expected_effective_at. Legacy reads, first creation, pause/enable and captured request settlement remain supported.

## [0.16.0] - 2026-10-30

### Added

- List reset cards and prioritize expiry (#137)
- Add configurable Turnstile login verification (#138)

## [0.15.0] - 2026-10-29

### Added

- Automate reset card redemption (#133)

### Changed

- Parallelize race tests and cancel superseded PR runs (#134)

## [0.14.0] - 2026-10-29

### Added

- Combine client version settings (#126)
- Filter personal statistics by API key (#128)
- Support removing workspace members (#131)

## [0.13.0] - 2026-10-24

### Added

- Add scheduled encrypted backups (#119)
- Add opt-in quota-weighted rotation (#120)
- Export request history and usage as CSV (#121)
- Notify at member allowance thresholds (#122)
- Expose protected Prometheus gateway telemetry (#123)
- Automate Compose credential-key separation (#124)

## [0.12.0] - 2026-10-23

### Changed

- Batch catalog and quota snapshots
- Batch catalog and quota snapshots (#112)
- Use indexed personal usage queries (#114)
- Batch account runtime snapshots (#115)
- Target capacity notifications by pool (#116)
- Gzip embedded static assets (#113)
- Lazy-load the Simplified Chinese dictionary (#117)

### Security

- **BREAKING:** Add MFA, passkeys and encrypted backups (#111) — New backup exports require a separate password. CLI callers must supply --password-stdin; export API callers must send JSON {password}. Encrypted verification and restore use password plus file multipart uploads. Historical unencrypted archives remain readable.

## [0.11.0] - 2026-10-23

### Added

- Abbreviate token totals with K/M/B (#109)

### Fixed

- Allow model switches on the bound account (#108)

## [0.10.0] - 2026-10-22

### Added

- Show hidden models last in browser catalogs (#104)

### Removed

- Remove unused screenshots (#105)

### Fixed

- Contain table scroll overflow (#103)
- Handle upload timeouts and record ingress failures (#106)

## [0.9.0] - 2026-10-22

### Added

- Add Magpie import and label key actions (#95)
- Add manual model checks (#96)
- Prefer parent accounts for Codex reviews (#98)
- Add 11-language dashboard support (#99)

### Fixed

- Preserve hidden model routing capabilities (#97)
- Separate internal model visibility from capabilities (#100)
- Separate hidden model visibility from capabilities (#101)

## [0.8.0] - 2026-10-19

### Added

- Unify table pagination and scrolling (#92)

### Changed

- Upgrade CLIProxyAPI to v8.0.35 (#93)

## [0.7.3] - 2026-10-19

### Fixed

- Add sanitized upstream failure diagnostics (#90)

## [0.7.2] - 2026-10-19

### Fixed

- Accept Claude Code system control turns (#88)

## [0.7.1] - 2026-10-18

### Fixed

- Allow portable conversation model switches (#86)

## [0.7.0] - 2026-10-16

### Added

- Hide revoked API keys by default (#83)
## [0.6.0] - 2026-10-13

### Added

- Add configurable resilience policies (#77)

### Fixed

- Synchronize official client versions (#78)
- Sync client versions and support adaptive thinking (#79)
## [0.5.0] - 2026-10-03

### Added

- Add workspace request content rules (#72)
- Add API channels and resource groups (#73)
## [0.4.0] - 2026-09-30

### Added

- Route Claude and Antigravity by subscription quota (#67)
- Isolate model rate limits and cooldown recovery (#68)
- Add updates for existing deployments (#69)
- Add model availability diagnostics and alerts (#70)
## [0.3.0] - 2026-09-29

### Added

- Add terminal selection menus (#58)
- Enable Claude and Antigravity subscriptions (#59)
- Add shared model price catalog (#62)
- Support Grok subscriptions and quota (#63)
- Add lightweight request diagnostics (#65)

### Changed

- Add Air live reload (#64)

### Fixed

- Report Claude and Antigravity subscription quotas (#61)
## [0.2.0] - 2026-09-29

### Added

- Support read-only demo deployments
- Add isolated read-only server demo (#54)
- Add guided setup and workspace alerts
- Check local ports before deployment

### Fixed

- Keep workspace alert settings readable
## [0.1.0] - 2026-09-29

### Added

- Add apply_now option and fix quota exhausted 403 status
- Show estimated USD cost
- Default member concurrency to 10
- Raise workspace request capacity to 30
- Add multi-client CC Switch import and Cline setup

### Fixed

- Load allowance recipients by workspace
- Record and display reasoning effort
- Allow configurable 128 MiB request bodies
- Align concurrency limits with persisted settings
- Preserve availability during credential refresh failures (#52)
## [0.1.0-rc.9] - 2026-09-27

### Added

- Guide Docker and binary deployments

### Fixed

- Harden hosted cleanup, limits, and price integrity
## [0.1.0-rc.8] - 2026-09-27

### Added

- Add configurable windows and pool pricing

### Changed

- **BREAKING:** Use explicit budgets and local reset schedules — Legacy upstream percentage allocation schemes are unsupported and must be removed and recreated.
## [0.1.0-rc.7] - 2026-09-25

### Changed

- **BREAKING:** Use resource allowances for token caps (#34) — Standard-key budget rules no longer limit requests, and the /api/me/budgets and /api/members/{id}/budgets endpoints are removed.

### Fixed

- Show workspace owners in management
- Honor owner pool access
## [0.1.0-rc.6] - 2026-09-24

### Added

- Combine overview and personal usage (#29)

### Changed

- **BREAKING:** Focus setup on Codex with searchable proxies (#28) — Existing Claude and Antigravity accounts cannot serve requests until those providers are re-enabled.

### Fixed

- Render invitation records (#30)
- Trim redundant console text (#31)
## [0.1.0-rc.5] - 2026-09-24

### Added

- Add one-time invitation registration (#22)
- Add fixed account proxy pool (#24)
- Add bulk model prices and expand saved rates (#25)

### Fixed

- Unify account pool terminology (#23)
- Rename Chinese allocation UI to usage allocation (#26)
## [0.1.0-rc.4] - 2026-09-23

### Added

- Show Codex reset cards in compact cards

### Changed

- **BREAKING:** Isolate tenant data and complete setup — Replace the pre-release SQLite migrations with one initialization schema. Existing development databases are not upgraded automatically; start with a fresh data directory. Personnel-team APIs and UI are removed.
## [0.1.0-rc.3] - 2026-09-22

### Added

- Add scoped member token budgets
- Add team access and exclusive usage allowances
- Select pool models and snapshot allowance rates
- Reconcile shares and allow idle borrowing

### Changed

- **BREAKING:** Use explicit pools and optional team limits — New keys require group_id or scheme_id. Accounts and members no longer receive automatic default-pool assignments.
## [0.1.0-rc.2] - 2026-09-21

### Added

- Add published Docker image installation
- Add native Claude and Gemini protocols
## [0.1.0-rc.1] - 2026-09-20

### Added

- Bootstrap SubLane foundation
- Add administrator onboarding and sessions
- Add gateway readiness and resilient status refresh
- Add member access and personal API keys
- Add subscription accounts and gateway forwarding
- Show subscription quota windows and reset times
- Persist and coalesce quota snapshots
- Add Claude and Antigravity subscriptions
- Add scoped account pools and member access
- Add pool runtime controls and scoped request history
- Add password management, request limits and usage charts
- Add key controls, model policies and audit logs
- Add CC Switch quick import
- Add account catalogs and Codex version settings
- Route native model IDs across providers
- Show input cache hit rates
- Add CLI and admin backup and restore
- Add request diagnostics and quota-aware routing

### Changed

- Compact subscription and quota layout
- Upgrade CLIProxyAPI to v7.3.7
- Add multi-platform packaging and publication
- Add guarded publish command and trim readmes
- Generate Keep a Changelog release notes

### Fixed

- Constrain quota column width
- Distinguish remaining quota with status colors
- Restore version-gated model discovery

[0.20.0]: https://github.com/murongg/SubLane/compare/v0.19.2...v0.20.0
[0.19.2]: https://github.com/murongg/SubLane/compare/v0.19.1...v0.19.2
[0.19.1]: https://github.com/murongg/SubLane/compare/v0.19.0...v0.19.1
[0.19.0]: https://github.com/murongg/SubLane/compare/v0.18.1...v0.19.0
[0.18.1]: https://github.com/murongg/SubLane/compare/v0.18.0...v0.18.1
[0.18.0]: https://github.com/murongg/SubLane/compare/v0.17.0...v0.18.0
[0.17.0]: https://github.com/murongg/SubLane/compare/v0.16.0...v0.17.0
[0.16.0]: https://github.com/murongg/SubLane/compare/v0.15.0...v0.16.0
[0.15.0]: https://github.com/murongg/SubLane/compare/v0.14.0...v0.15.0
[0.14.0]: https://github.com/murongg/SubLane/compare/v0.13.0...v0.14.0
[0.13.0]: https://github.com/murongg/SubLane/compare/v0.12.0...v0.13.0
[0.12.0]: https://github.com/murongg/SubLane/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/murongg/SubLane/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/murongg/SubLane/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/murongg/SubLane/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/murongg/SubLane/compare/v0.7.3...v0.8.0
[0.7.3]: https://github.com/murongg/SubLane/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/murongg/SubLane/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/murongg/SubLane/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/murongg/SubLane/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/murongg/SubLane/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/murongg/SubLane/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/murongg/SubLane/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/murongg/SubLane/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/murongg/SubLane/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/murongg/SubLane/compare/v0.1.0-rc.9...v0.1.0
[0.1.0-rc.9]: https://github.com/murongg/SubLane/compare/v0.1.0-rc.8...v0.1.0-rc.9
[0.1.0-rc.8]: https://github.com/murongg/SubLane/compare/v0.1.0-rc.7...v0.1.0-rc.8
[0.1.0-rc.7]: https://github.com/murongg/SubLane/compare/v0.1.0-rc.6...v0.1.0-rc.7
[0.1.0-rc.6]: https://github.com/murongg/SubLane/compare/v0.1.0-rc.5...v0.1.0-rc.6
[0.1.0-rc.5]: https://github.com/murongg/SubLane/compare/v0.1.0-rc.4...v0.1.0-rc.5
[0.1.0-rc.4]: https://github.com/murongg/SubLane/compare/v0.1.0-rc.3...v0.1.0-rc.4
[0.1.0-rc.3]: https://github.com/murongg/SubLane/compare/v0.1.0-rc.2...v0.1.0-rc.3
[0.1.0-rc.2]: https://github.com/murongg/SubLane/compare/v0.1.0-rc.1...v0.1.0-rc.2
[0.1.0-rc.1]: https://github.com/murongg/SubLane/releases/tag/v0.1.0-rc.1

<!-- generated by git-cliff -->
