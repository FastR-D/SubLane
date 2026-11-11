---
name: SubLane
description: A minimal, restrained interface for an internal team gateway.
colors:
  primary: "#202020"
  primary-foreground: "#FFFFFF"
  background: "#F7F7F7"
  surface: "#FFFFFF"
  foreground: "#171717"
  muted-foreground: "#616161"
  muted: "#EEEEEE"
  border: "#E5E5E5"
  input: "#8A8A8A"
  success: "#15803D"
  success-muted: "#F0FDF4"
  warning: "#92400E"
  warning-muted: "#FFFBEB"
  error: "#B91C1C"
  error-muted: "#FEF2F2"
  info: "#1D4ED8"
  info-muted: "#EFF6FF"
  dark-primary: "#F5F5F5"
  dark-primary-foreground: "#171717"
  dark-background: "#101010"
  dark-surface: "#191919"
  dark-foreground: "#F5F5F5"
  dark-muted-foreground: "#A3A3A3"
  dark-muted: "#262626"
  dark-border: "#333333"
  dark-input: "#777777"
  dark-success: "#86EFAC"
  dark-success-muted: "#142C1F"
  dark-warning: "#FCD34D"
  dark-warning-muted: "#332A14"
  dark-error: "#FCA5A5"
  dark-error-muted: "#351C1C"
  dark-info: "#93C5FD"
  dark-info-muted: "#1C2A40"
typography:
  title:
    fontFamily: "-apple-system, BlinkMacSystemFont, Segoe UI, PingFang SC, Microsoft YaHei, sans-serif"
    fontSize: "24px"
    fontWeight: 600
    lineHeight: 1.3333
    letterSpacing: "-0.025em"
  body:
    fontFamily: "-apple-system, BlinkMacSystemFont, Segoe UI, PingFang SC, Microsoft YaHei, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.4286
  label:
    fontSize: "12px"
    fontWeight: 500
    lineHeight: 1.3333
rounded:
  sm: "6px"
  md: "8px"
  lg: "10px"
  xl: "14px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "32px"
  page: "40px"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.primary-foreground}"
    rounded: "{rounded.md}"
    height: "36px"
  status-success:
    backgroundColor: "{colors.success-muted}"
    textColor: "{colors.success}"
    rounded: "{rounded.md}"
    padding: "4px 8px"
---

# Design system: SubLane

## Overview

SubLane is an operational interface: clarity, familiar controls, and reliable feedback take priority. Its confirmed direction is minimal and restrained, using Shadcn Admin primitives with a black, white, and neutral gray identity.

The implementation lives in `web/src/styles.css` and `web/src/components/`. This document records that system; update the tokens and this document together when an intentional design change is made. There are no marketing illustrations or decorative charts in the foundation.

## Colors

Primary actions, navigation, focus, and selection use neutral colors. Colored accents have a stable semantic role: green for success, amber for warnings, red for errors, and blue for information. Paused or unconfigured states remain neutral.

Use the light and dark token pairs above. Dark mode changes both foreground and surface colors; it does not apply a blanket inversion. The selected surface uses the slightly raised neutral accent token, while badges use their own tinted background.

Reserve `--destructive` for destructive controls. Error text and badges use `--error`, which has a lighter dark-theme foreground. Always pair status colors with text and, where helpful, an icon. Never rely on hue alone to communicate a state.

Quota bars and remaining percentages use success at 20% or more, warning below 20%, and error below 5%. Unknown quota stays neutral and has no bar. These semantic tokens adapt to both themes; numeric labels remain visible.

Keep text contrast at least 4.5:1 and visible control boundaries/focus indicators at least 3:1 against adjacent colors. Subtle structural dividers are not interactive control boundaries.

## Typography

Use the platform sans-serif stack for fast rendering and native multilingual typography. No remote font service is required.

- Page titles use the title role, balanced wrapping, and restrained tracking.
- Section headings use normal-size text with medium weight.
- Descriptions and most controls use the body role. Explanatory copy may use a 24px line height.
- Secondary metadata and status badges use the label size; avoid reducing normal reading text below 12px.
- Use tabular figures for changing durations and other aligned numeric values.

Keep all visible UI copy and accessibility labels in the translation dictionaries. Allow translations to wrap naturally, including longer European-language phrases; do not truncate important status or recovery information.

## Layout

The desktop shell follows the user-selected Shadcn Admin layout: a compact two-line brand block, a 256px sidebar, a 64px minimum-height header, and a fluid main area capped at 1152px. Main content uses 24px padding on larger screens and 20px horizontal padding on small screens. The sidebar ends with a compact account trigger containing initials, username, and actual account role. Keep SubLane's neutral palette and real instance data.

Request history, audit records, members, API keys and model prices use the full available main width. Reuse `TableViewport` for a viewport-bounded scrolling area with a sticky header and `Pagination` outside it, so horizontal scrolling remains accessible with long result pages. Show the filtered total, visible record range, current and total pages, 10/25/50/100-row choices, bounded page numbers and direct page entry. Use the existing custom dropdown pattern for page size. On narrow screens, retain the total, page size, previous/next controls and direct page entry while hiding the numbered strip; model prices retain their existing stacked rows. Filters and page-size changes return to page one; deleted or expired records clamp to the last valid page. Page changes reset vertical table scrolling while preserving the horizontal position. On short screens or with expanded filters, allow the main area to scroll rather than clipping controls or collapsing the table.

Below 768px, navigation becomes a 288px modal sheet opened by the header trigger. Main content takes the full width. Lists of details and toolbars wrap or stack; the service details become a single column below 640px.

Personnel team member and allowed-pool checklists and allocation scheme fields use a single column on narrow screens, expanding into two columns at 640px; settlement fields expand into three. Member allowance rows keep the amount and unit together while long usernames wrap. Keep these forms inline and bounded, with divided rows and wrapping actions rather than nested cards.

Use spacing to group related information. The home page leads with personal usage and keeps account, client, or gateway access information concise. Administrators see a compact gateway prompt only while setup is incomplete. Full gateway setup guidance and instance health live on the administrator **Instance status** page. Show readable uptime and the last successful check time there. A failed refresh keeps the previous readings visible, adds an explicit warning, and marks current service health as unknown. Do not add dashboard metrics without real data. Language, theme, and a settings shortcut appear as icons at the top right on authenticated pages, including mobile. The sidebar groups common pages above administrator tools, including a separate **System settings** page for instance-wide configuration. The header settings shortcut opens personal **Preferences**. Give icons accessible names and hover descriptions, and reflect the selected theme with a sun, moon, or monitor. Setup and login share the same language and theme icon controls in a quiet 64px header. The Preferences form uses triggers with visible text.

System settings uses a collapsible sidebar branch with **Workspace alerts**, **Time zone**, a single platform-owner **Client versions** entry at `/admin/settings/clients`, and **Backup and restore**. Use the existing submenu primitives, an expansion chevron, and a subtle indentation guide. Highlight the active child; keep the parent as a keyboard-operable disclosure button with `aria-expanded` and `aria-controls`. Selecting a child closes mobile navigation. A direct child URL opens its branch. The original system-settings URL and saved Codex, Grok, and Claude settings URLs redirect to Client versions. That page stacks all three providers using `ClientVersion` with provider-specific copy, independent form state, and separate save/check actions. Keep its flat divided sections, neutral effective/latest-version summaries, labeled manual-version fields, automatic-check checkboxes, wrapping actions, and secondary check metadata. The summaries use two columns on wide screens and stack on narrow screens; preserve localized loading, validation, success, failure, and disabled-action feedback.

## Elevation & Depth

Surfaces are mostly flat. Distinguish the canvas, sidebar, and content with neutral surface tones and subtle borders. The adapted button primitives retain small interaction shadows; sheets use the upstream overlay and shadow to establish a modal layer. Avoid decorative glows and heavy card shadows.

## Shapes

Use gently rounded controls and panels from the existing radius scale. Badges are compact rounded rectangles. Primary controls and major surfaces should not acquire unrelated corner styles. Keep icons from Lucide with consistent stroke treatment; do not use emoji as interface icons.

## Components

### Logo

Use the approved original mark: two offset lane segments forming an S. The canonical vector is `docs/assets/logo.svg`; `docs/assets/logo-dark.svg` uses the same paths with a light fill for dark surfaces. Keep the transparent background and square viewBox. Preserve the proportions and path geometry when resizing or recoloring.

The repository keeps only the README lockups, the selected dark social preview, color values, and font sources in `brand/`. Run `make brand` to generate the complete asset set in Git-ignored `dist/brand/`. The README uses the transparent horizontal lockup pair in `brand/logos/` at 420px, followed by compact badges. Technology and license badges stay neutral; the foundation-stage badge uses the warning color. Follow `brand/README.md` for clear space and format selection.

The web app ships the generated favicon SVG, multi-size ICO fallback, and Apple touch icon in `web/public/`. The SVG uses a dark mark in light browser chrome and a white mark in dark chrome; the fallback icons use the approved monochrome tile. Keep these assets synchronized with the canonical mark when branding changes.

The graphic wordmark uses Inter 650 at optical size 32 with -0.025em tracking, exported as paths for portability. This branding font does not replace the application's platform sans-serif stack. Keep supporting graphics monochrome and use semantic colors only for status.

### Buttons and selectors

Use `Button` for primary, outline, and ghost actions. Primary controls reverse from dark-on-light to light-on-dark with the theme. Language and theme choices use the adapted Shadcn Admin `DropdownMenu` with radio semantics, a checkmark on the selected item, neutral hover/focus backgrounds, and theme-aware popover surfaces. Do not use browser-native select popups. Text triggers retain a 16px Lucide chevron and a clear gap after the selected label; header triggers show only the language or current-theme icon. Preserve keyboard navigation, Escape dismissal, and return focus to the trigger.

Desktop controls are compact. Coarse-pointer targets expand to at least 44px tall and editable controls use readable text. Preserve loading and disabled states, and never show an enabled action that cannot perform useful work.

### Navigation

Workspace content rules live at `/admin/content` in the Administration navigation. The page places refresh/configure or cancel/save actions in the header, with workspace policy on the left and a bounded rule list/editor on the right at desktop widths. Mode edits use a native keyboard-operable three-option radio control; rule types keep the shared dropdown. On narrow screens, policy metadata uses two compact columns and status badges sit beside wrapping rule names. Scope and detailed limits live in a disclosure below the primary controls. The page reuses the system typography and flat divided rows. A focused inline editor changes the workspace mode and up to 50 rules; saved patterns are never loaded into the browser, blank edits retain them, and literal terms use masked inputs, and newly entered regex drafts use a local syntax-highlighted native textarea. The decorative token layer is hidden from assistive technology, scrolls with the textarea and never rewrites input or inserts highlighted HTML; backend Go RE2 remains authoritative. Each rule has a disclosure for transient sample testing. Rule controls are disabled while a test runs; success and failure both clear the sample, leaving only match feedback or a localized error. Observe mode explicitly explains that matching content still goes upstream. Request details display bounded policy versions and rule IDs, with an administrator-only recovery link to the rules page.

Navigation follows persisted roles. The upper **General** group contains the shared overview with personal usage, personal API keys, personal request history, and Model prices (`/pricing`), available to both administrators and members; the lower **Administration** group contains subscription accounts, API channels, resource groups, all request records, team usage, instance status, and member management and is hidden entirely for members. The overview shows a personal workspace for members and a compact gateway prompt for administrators when needed. The former personal usage URL redirects to the overview. API key management is common to both roles and shows only the current user’s records. Revoked keys are hidden by default; a labeled Show revoked switch stays available in loading, empty, and failed views. Paused and expired keys remain visible. Filtering happens before server pagination, and changing the filter or revoking a key returns to the first page. The filtered empty state offers creation or viewing past keys; successful revocation has persistent inline feedback. Client configuration is opened on demand from a secondary Setup guide button beside Create key, using a right-side sheet with a fixed header and scrollable content. The closed guide takes no space below the key list and does not poll connection state; creation displays the full secret in a one-time dialog, and revocation requires confirmation. Both roles retain personal preferences in the header, including a password-change action. Password dialogs explain that all browser sessions will end while gateway keys remain valid. Member request limits and password reset use an overflow menu beside the existing row actions; dialogs restore focus to that trigger. Direct access to an administrator route shows a localized access-denied page before management content mounts. Member management uses a paginated table and a focused creation sheet; status changes show clear pending and error feedback.

Reuse `SidebarProvider`, `Sidebar`, `SidebarMenuButton`, and `SidebarTrigger`. Active routes have a neutral selection surface and `aria-current`. Close the mobile sheet after navigation and retain keyboard focus handling from Radix. The account trigger opens a Shadcn Admin-style menu beside the desktop sidebar or above the trigger on mobile. Show the real username, account role, and a sign-out action; keep pending and failed sign-out feedback visible in the sidebar.

### Authentication

Optional Turnstile verification appears before the sign-in button and uses the active theme, flexible desktop width and compact mobile layout. Password and passkey controls wait for a valid challenge token; failed submissions refresh it, and widget loading failures offer an inline retry. The platform-owner Turnstile page under System settings uses the existing flat form layout, a verification checkbox, Site Key and masked Secret Key fields, persistent save feedback, and an empty secret field after saving. Explain that leaving the secret blank preserves it and that the login hostname must be configured in Cloudflare.

First-time setup starts with a centered welcome surface, the approved mark, a brief introduction, and a two-step outline of administrator creation and workspace entry. **Start setup** opens the administrator form on its own route; the form offers a return action until submission is in progress. Move focus to the page heading when moving between setup screens, and sign in automatically after successful creation. Initialized instances skip the welcome flow.

Setup and login share the workspace's compact icon controls, custom dropdown menus, typography, and neutral palette. Keep the header transparent and borderless, and the centered content surface focused on the current step. Forms retain visible field labels and localized inline errors. Dark inputs use the canvas background to retain contrast against the input-border token. The private navigation is rendered only after authentication; the sidebar shows the current user and a sign-out action.

### Status

Use the shared `Status` component for success, warning, error, information, or neutral badges. It pairs an icon with text and a theme-aware semantic foreground/background. A temporary loading state uses a labeled spinner and respects reduced motion.

### Data, errors, and empty states

System data comes from the backend. Show a loading state while connecting, an actionable error with retry on failure, and an explicit empty state for unavailable account integration. Never fill these states with invented account counts or usage charts.

The account connection form presents Codex, Claude, and Antigravity as three equal-width provider choices with their original full-color brand logos. Only Codex is selectable; the other two choices remain visible but disabled. Preserve a neutral selected border/background, a checkmark, keyboard focus, and accessible pressed state. Existing Claude and Antigravity accounts remain visible with a temporarily disabled status and can be deleted. Provider brand marks are an intentional exception to the monochrome supporting-graphics rule; do not recolor them with interface status tokens.

Network proxy selection in account connection and rebinding uses a searchable, keyboard-operable dropdown. Include the default network route as a choice; keep long proxy names readable and the menu bounded on narrow screens.

Subscription accounts use a compact list: identity and plan, quota windows, account status with token expiry, and icon actions align in columns when the list has enough width. Narrow containers stack the same content in reading order. Keep each quota window within its column; a single window must not occupy a separate full-width panel. Snapshot time remains secondary, and icon actions retain accessible names and hover descriptions.

Account pools use a compact list with names, enablement, account/member counts, and a clear edit action. No pool has a protected default role. Newly imported accounts show Unassigned with a link to account-pool setup until explicitly assigned. Pool editors use a bounded account checklist. Member grants live in the Members row action; personal key creation shows only authorized pool choices through a custom dropdown. Explain revocation and shared pool membership before applying changes.

Pool access is granted directly to members; ordinary sharing needs no allowance. The member list includes the workspace owner, administrators, and ordinary members. The owner remains protected from status and role changes. Joining an existing login uses its username and an explicit workspace role. Changing a role must preserve the membership's enabled state; creating a new member or joining an existing one offers an immediate pool-grant action. Personal key creation requires an explicit pool choice, shows each pool's account count, and warns when a selected pool is empty. The overview marks key setup ready only when an active key can reach a ready account through its current pool and allowance binding.

Optional resource allowances use dedicated pools and present percentage shares, direct internal USD amounts, direct token limits, and configurable time-window USD limits as mutually exclusive choices. Share mode chooses a Token/internal USD unit and either a calendar total or up to eight elapsed-window totals before assigning common member percentages. Time-window mode accepts up to eight whole-hour or whole-day conditions with shared USD defaults for selected members and per-condition personal overrides; each member is metered independently, and an empty limit is unlimited. Daily/monthly rules choose a reset clock or date and clock in the instance time zone; duration windows begin at activation and renew independently by elapsed time. Pool and reset-period selectors reuse the custom `DropdownMenu` radio pattern with an outline trigger, selected checkmark, and chevron. Keep fixed pool choices visible but disabled when editing. Group rule details, allowance fields, member selection, and activation into flat sections with dividers; keep the incumbent background. Put name and pool side by side on wide screens, arrange mode choices in two columns, align member inputs, and provide a select-all checkbox for each mode. Show M, USD, or % beside inputs and allocated/unallocated percentages below share inputs. For priced rules, snapshot prices from models supported by the selected pool without asking for prices in the allowance form. Keep internal budgets distinct from upstream capacity and cash balances.

Allocation balances use divided rows with tabular allowance, used, and remaining values. Show reset timestamps in the configured instance time zone independently of status. Time-window balances group every configured condition under one member, identify which condition blocks admission, show an unlimited condition without a zero remaining amount, and derive the member status from the most restrictive limited condition. Ratio balances use the selected amount or token unit after multiplying the total by the member's percentage. Use the shared `Status` component: unavailable is neutral, new-request risk pauses are warning, exhausted is error even when reconciliation is pending, and active is success. Current-window active/pending counts, provisional reservations, and admission headroom explain a risk pause without presenting reserved capacity as spent usage; pending entries outside a window do not change that window's status. Current scheme details remain distinct from a secondary next-cycle mode/effective-date notice. The current-member editor retains usage and captured reservations and exposes only member inputs. Ordinary budget/price changes stay scheduled without restarting intervals; structural changes require explicit new-period confirmation. Access enablement stays a separate control. Forms retain reviewed version conditions through background refresh and offer explicit refresh/review after conflicts.

The allocation edit form shows the server-calculated effective date before saving. For a new time-window rule scheduled to start next period, preview the date from its longest entered condition. Surface live price-coverage warnings near the current rule and in the edit form: current catalog prices win, previously saved prices cover gaps, and models missing from a revision's saved rates remain blocked until a new revision takes effect. Keep catalog-price gaps, saved-rate gaps, and temporarily unavailable account catalogs distinct; recompute these warnings on reads instead of storing them in rule config. Keep the distinct blank-value explanations beside direct member inputs and time-window condition inputs.

An uncovered-model warning includes the server-calculated edit effective date, so administrators can see when saving a price update will actually permit that model. For an already scheduled revision, use its saved effective date and past-tense scheduling copy.

Administrator allocation reports expose refresh and inline Token correction forms beside pending request IDs, with explicit M units and error feedback. Personal allocations reuse the balance presentation without editing or settlement controls, with visible loading, failure, empty, and refresh states.

Account scheduling metadata stays compact beside account status. Settings use a focused dialog; limits and cooldown controls do not replace authentication status. General → Requests shows the current user’s calls, with key/pool metadata and a result filter. Administration → All requests adds caller/subscription-account information and an account filter. Both tables use bounded pagination and explicit unknown token values; the personal page never fetches subscription-account management data. Keep source content and credentials out of logs and UI.

The personal usage section on the overview and the team usage page share the established request-table layout. A flat metric strip shows real counts, completion rate, mean duration and reported input/output tokens before personal allowances. Custom selectors choose the period, breakdown and request/input-token/output-token metric. Today and the 7/30/90-day periods use calendar-day boundaries in the configured instance time zone, including 23/25-hour days and skipped midnight. Daily trends use a neutral interactive line chart; model, pool and member comparisons use horizontal bars ordered by request count. Use responsive SVG geometry with readable axis labels, pointer/touch and keyboard inspection, and gaps for unknown token reports. Exact tables expand on demand through View details. Display the collection start in the configured instance time zone, hide unobserved earlier days, label partial token totals, and preserve the unknown state instead of inventing zeros. Personal usage adds a compact read-only limit summary; team usage also offers member breakdowns.

Personal usage adds an All keys selector beside the period. Load key metadata only when opened, include revoked keys with a status label, and keep every result page reachable with retry feedback. Selecting a key updates the metric strip, all chart breakdowns, detail tables, and hourly heatmap together; keep the selection when the period changes. Display the separate collection start for attributed key data, preserving earlier unattributed usage in All keys. Personal allowances and member limits retain their own user-level scope.

#The full-width Activity by time card sits below the primary usage chart and above expanded detail tables in both usage views. Use a Monday–Sunday by 00–23 grid in the configured instance time zone, with borderless square cells, neutral intensity levels, a fainter uncollected/unreported fill, and visible exact values on pointer/touch or keyboard selection. It follows the selected period and metric without changing with the primary breakdown. Keep labels readable and scroll only the grid horizontally on narrow screens; the detail footer remains outside the scrolling area. Show the independent hourly collection start and the grid label in the aggregation time zone returned by the server; never present pre-collection or future hours as zero. Refresh the activity when the instance time zone changes. Preserve historical hourly activity where local hours align with UTC hours; use minute aggregates where offsets or date boundaries split a stored hour, so earlier calls cannot leak into the selected period. Show the independent minute-collection start; legacy UTC daily totals remain preserved and are not fabricated into local dates. Use the returned report zone for all chart/table dates and CSV metadata, and use the next day boundary rather than assuming 24 hours when filtering uncollected days.

Personal key rows offer a compact Change group icon button beside the current group, retaining a localized tooltip and accessible name. Its focused dialog loads only authorized choices, includes allowance names, and explains that the key remains unchanged, existing usage stays in the original group, and switching requires a new conversation. A missing allowance binding has its own status and a Restore access action with confirmation; unavailable membership instead provides administrator guidance. Keep loading, retry, pending, failure and persistent success feedback explicit. New allowance forms explain automatic binding of selected members’ existing unrevoked keys and loss of access for excluded members before saving.

## Motion

Keep motion limited to state changes, the sidebar transition, and loading feedback. Respect `prefers-reduced-motion`. There are no entrance animations or looping decorative effects.

## Do's and Don'ts

- **Do** reuse the semantic tokens and adapted Shadcn Admin primitives.
- **Do** verify both themes, both languages, keyboard navigation, and narrow layouts.
- **Do** keep primary controls neutral and reserve color for meaningful status.
- **Do** distinguish a planned capability from a working action.
- **Don't** add decorative gradients, vivid dashboard fills, or arbitrary accent colors.
- **Don't** display fake business records or performance claims.
- **Don't** rely on color, hover, or icons alone for essential information.
- **Don't** expose SDK configuration details in routine team workflows unless users need them to make a decision.

Model catalogs use compact **Models** actions on accounts, pools and personal keys. Account dialogs distinguish unknown support, a known empty catalog, stale saved data and refresh failure, and show the last successful observation time. Pool catalogs are read-only derivations of enabled accounts and existing policy. Keep searches and bounded lists inside the dialog. Include hidden capabilities in browser catalogs with a compact neutral “Hidden model” text badge beside the native ID; place them after visible models, keep alphabetical order within each group, and count and search them normally without treating the visibility label as an availability result. Preserve client discovery and picker filtering. CC Switch uses an accessible custom combobox over the selected key’s catalog; do not retrieve secrets to populate model choices. Show native model IDs without provider prefixes, with identical IDs appearing once across providers.

### Backup and restore

Keep export and restore in two flat sections under System settings. State that archives contain credentials. Verification shows creation time, SubLane version and database size; restoration prepares a new directory and shows the path with explicit stop/configure/restart steps. An existing target blocks overwriting. Disable dependent actions until verification succeeds, clear verification when the file changes or is checked again, and retain honest error/permission/loading states in both languages.

### Request diagnostics

Keep account/result filters in the compact request toolbar. Additional model, request ID, caller and local-time filters expand inline and use the existing custom dropdowns. Show first-output latency as secondary text under total duration; null measurements remain an em dash. Show a compact, selectable request ID with a copy action below each start time; use an em dash when older records have no ID. The start-time button opens a focused request details dialog with a selectable/copyable correlation ID. On narrow screens, filters stack and the data table scrolls inside its own container. Quota exhaustion uses a warning status and explains automatic switching to an eligible account. Recovery guidance explains when no eligible account remains.

Place routing explanations in a flat divided section inside request details. Show the recorded strategy, final candidate count and nonzero aggregate exclusion counts with wrapping labels. Older or unrouted records explicitly state that no recorded explanation is available. Personal views never add account identities.

Model rows show upstream-reported capabilities in a compact definition list below the native ID. Numeric limits use localized tabular figures; unknown values remain explicitly unknown and reported false values read as unsupported. Keep existing search, hidden-model badges and model-test actions. Explain conservative resource-group merging beside the catalog, without suggesting that capability metadata grants access.

Quota usage pace uses neutral wrapping text below the existing reset information. A keyboard- and touch-accessible disclosure explains its observation time and reference semantics. Preserve remaining-quota bar colors, and omit pace when the snapshot or window cannot support the comparison.


### First-call walkthrough

The overview offers a compact, optional entry to a floating walkthrough. Highlight the existing control for the current step and position a readable, dismissible panel nearby, keeping it inside the viewport. Keep the underlying page usable. Hide the panel while a dialog or menu is open, preserve newly created keys until the user closes their dialog, and resume afterward. Back and skip stay available; advancing requires server-confirmed completion. Unknown progress never implies success. Keep keyboard focus, Escape dismissal, localization, and narrow-screen placement consistent with the rest of the application.


### API channels and resource groups

Subscription accounts (`/accounts`) and API channels (`/channels`) are separate administrator surfaces. Subscription creation offers OAuth or credential JSON for subscription providers only. Channel creation uses a dedicated `/channels/new` page with platform, upstream protocol, name, base URL, masked API key and network proxy. Searchable, keyboard-operable platform and protocol dropdowns share the proxy picker interaction. Platform search matches the displayed name, platform ID and preset endpoint, resets on reopening and preserves the current selection until confirmed. Selecting a platform fills the default endpoint and protocol; Custom keeps the endpoint editable and offers all supported upstream protocols. Long platform lists scroll within the menu. Platform choices and channel rows use locally bundled brand icons, with a neutral plug for Custom. Key replacement stays in a focused dialog with an empty secret field and read-only platform, protocol and endpoint. Channel lists show endpoint, protocol, actual credential state, observed activity and resource-group assignment, without subscription quota or token-expiry UI. Missing runtime observations stay unknown.

Resource groups retain `/groups` and existing key bindings. The editor presents subscription accounts and API channels in separate bounded lists, followed by routing preference and model policy. New groups prefer subscriptions and leave API fallback unchecked. Mixed groups explain the fallback condition; API-only groups explicitly use their configured channels. Source removal and routing restrictions preserve conversation identity by failing affected continuations instead of moving them. Navigation, member grants, key configuration, diagnostics and onboarding use the resource-group terminology. Initial activation offers both subscription and API-channel setup.

### Allowance scheduling controls

Keep pending cancellation and postponement inline with the existing allowance row. Initial-only rules disable cancellation and explain pause/reschedule alternatives. Preview the server-computed successor date before confirming a move; keep the reviewed version/time together. Delayed creation likewise shows the server start time before confirmation. Before the first activation, enable initial member correction in the full editor and explain that the reviewed time and anchor stay fixed. For an approved pending new structure, offer the existing new-period confirmation again to edit its members; ordinary budget/price edits keep it unchecked. Withdrawing optional reconfirmation shows the saved member instruction rather than a locked unsaved draft. Show shared-budget blocking without exposing another member's usage. A failed confirmation keeps the reviewed draft and requires explicit review rather than automatic rebasing.

### Group scheduling preferences

Group editors show preference boosts only for selected resources, with an accessible numeric percentage input from 0 to 300 and a reset action. Explain that the boost multiplies automatically calculated preference and cannot bypass admission or move existing conversations. Preserve unsaved values on request failure and keep legacy omitted values intact on unrelated edits.

The group Scheduling dialog shows an administrator-only snapshot for new Responses placement. An optional model filter and explicit refresh avoid provider calls. Display eligible resource names, target placement share, effective weight, manual boost, shared concurrency, quota/load factors and the original observation time. State when dynamic weighting is disabled, when quota observations use a fallback, and when no resource is eligible. Shares are placement targets before parent preferences and key allowance checks, not reserved capacity. Use flat divided rows, wrapped labels and both theme tokens; hide stale results while a refresh fails.


The Scheduling overview at `/groups/scheduling` reuses the standard dropdowns, numeric inputs, buttons, table viewport, pagination and the shared interactive time-series chart extracted from usage trends. Current distribution, history and parameters are separate views. Keep group/model context visible, explain quota fallbacks, and show excluded accounts without assigning them a target share. Draft previews show current-to-draft weight/share changes from one local snapshot; edits clear an obsolete preview. Explicit review reloads the latest revision without silently rebasing a conflicted draft. Numeric parameters include a small multiplier table over the reward horizon.

History scopes shared quota/pressure separately from group requests across all models. Resource, window, routing scope and metric selectors prevent unrelated observations from becoming one line. Preserve gaps across reset/lifecycle changes and missing observations, and mark partial token reporting. Evaluation shows observed and complete cycle counts, cohort coverage, policy comparisons and last balances with observation age. Insufficient coverage cannot imply zero waste or a proven optimum; suggestions are manual review prompts with an observational-evidence explanation.
