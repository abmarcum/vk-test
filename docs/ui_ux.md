# UI/UX Design Specification (Revised)
## Simple Video Streaming Application — Minimal Web Player (Stretch Scope)

**Version:** 3.0 (Revised per PM Conditional Approval — Round 2)
**Author:** UI/UX Designer Agent
**Status:** Ready for Frontend Implementation — Tiered Delivery Plan
**Revision Summary:** Addresses all five PM findings from the v2.0 review: (1) applies the "proposed convention" disclaimer to the Tier 1 file path for consistency with TD-5; (2) adds an executive effort-disclosure note flagging that Tier 2 was not requested by the PRD; (3) explicitly routes the API-key/`<video>` compatibility question to the Software Architect/Backend Engineer agent via a new Cross-Team Open Questions Tracker; (4) escalates the pagination contract ambiguity to the same tracker as a PRD clarification request rather than resolving it unilaterally; (5) surfaces the `<video>` `onerror` handling note earlier (in §2, not only §11) and adds a minimal implementation. No requirement changes — this is a consistency and process-routing pass only.

---

## 0. Changelog & Source-of-Truth Correction

### 0.0 Executive Effort Disclosure (New in v3.0 — PM Finding #2)

**For PM/Engineering visibility before backlog triage:** Tier 2 of this document (§3–§8, §11) represents a substantially complete design system — visual tokens, component hierarchy, responsive rules, accessibility audit, and state matrices — built for a feature the PRD explicitly frames as *optional/stretch* and adjacent to a stated non-goal ("No web frontend UI," §1.4). **This effort was not requested by the PRD and was produced speculatively, on the assumption that a coherent design would be useful if Engineering ever chooses to build past Tier 1.**

This is flagged here explicitly so the swarm can make a conscious, informed decision:
- **Accept Tier 2 into backlog** as-is if the "minimal web player" stretch goal is later prioritized, or
- **Discard Tier 2 entirely** with no loss to core deliverables — Tier 1 alone fully satisfies PRD §4.5 and §13.

Either decision is fine. The goal of this note is only to keep the swarm's **effort-to-requirement ratio** visible to the PM, per the review request, not to argue for Tier 2's inclusion.

### 0.1 What changed from v1.0 → v2.0

| # | PM Finding | Resolution in v2.0 |
|---|---|---|
| 1 | Citations to "Architecture §2" / "Architecture §3.1 `streamAuthMiddleware`" are unverifiable against the provided PRD | All such details marked **"Proposed Convention — Pending Engineering Confirmation."** Only the exact PRD §4.5 quote treated as ground truth. |
| 2 | Query-param API key fallback (`?api_key=`) is a new backend requirement, not in PRD | Called out explicitly as a **⚠️ Required Engineering Sign-Off** item, not silently assumed. |
| 3 | Spec exceeds "minimal stretch" mandate | Restructured into **Tier 1 (PRD-required minimum)** and **Tier 2 (optional enhancement, cut first)**. |
| 4 | 3-file structure implied as PRD-mandated | Relabeled as a **design recommendation** for Tier 2 only. Tier 1 ships as one self-contained HTML file. |
| 5 | Pagination model (cursor vs. limit/offset) unresolved | Marked **pending API contract confirmation**, both UI variants specified. |
| 6 | `MAX_UPLOAD_SIZE_MB` drift, content-type badge mapping | Moved into a formal **Tracked Tech-Debt Register**. |

### 0.2 What changed from v2.0 → v3.0 (this revision)

| # | PM Finding | Resolution in v3.0 |
|---|---|---|
| 1 | Tier 1 file path (`web/player.html` header in §2) lacked the same "proposed convention" qualifier applied to Tier 2 paths (§3.1), inconsistent with TD-5 and §0.2's own stated principle | §2 now explicitly labels the filename/path as a **suggested convention, pending confirmation against the actual `main.go`/static-serving setup** — same treatment as Tier 2. TD-5 wording updated to reference both tiers. |
| 2 | Tier 2's scope/effort relative to an "optional/stretch" PRD item wasn't surfaced for a conscious accept/discard decision | Added **§0.0 Executive Effort Disclosure** (above), at the top of the document, ahead of all design content. |
| 3 | §6 prescribes backend/security behavior (query-param auth, key storage), which slightly exceeds UI/UX scope | §6 now includes an explicit **"Routing"** line forwarding this item to the Software Architect / Backend Engineer agent for disposition, and it is logged as **OQ-1** in the new §14 Cross-Team Open Questions Tracker rather than left as a standalone in-document open item. |
| 4 | Pagination contract ambiguity (§5.2) should be escalated as a PRD clarification, not resolved unilaterally later by Designer or Engineer | §5.2 now includes an explicit escalation line, logged as **OQ-2** in §14, directed to PM/Architect for a PRD-level decision. |
| 5 | Tier 1's lack of `<video>` `onerror` handling was only mentioned in §11 (Non-Requirements/States), risking it being missed by whoever implements Tier 1 standalone | §2's "Notes for Engineering" now states this up front, and the Tier 1 code sample includes a minimal `onerror` handler so the behavior is not silently absent. |

### 0.3 Source-of-truth statement (unchanged from v2.0)

The **only** confirmed source text governing this deliverable is:

> PRD §1.4 (Non-Goals): *"No web frontend UI."*
> PRD §4.5 (Optional/Stretch): *"Minimal static HTML page with `<video>` tag pointing to stream endpoint."*

No file path, no serving mechanism, no auth middleware name, and no multi-file structure is confirmed by this text. Everywhere this spec references such details (file paths, `http.FileServer`, middleware names, query-param auth), they are explicitly labeled **"Proposed Convention"** and require sign-off from the Software Architect / Engineering before implementation — never treated as settled fact, **including the Tier 1 filename itself** (see §2).

---

## 1. Tiered Scope Model

To keep this deliverable proportionate to its "optional/stretch" status, the design is split into two independently shippable tiers. (See §0.0 for a candid effort-disclosure note on why this split exists.)

### Tier 1 — Required for Acceptance Criteria (ship this first, ship this alone if time-constrained)

Satisfies PRD §4.5 literally and supports the manual seek/playback acceptance test (PRD §13) with **zero extra scope**:

- **One** static HTML file.
- One `<video controls>` element pointing at `GET /videos/{id}/stream`.
- Video ID supplied via URL query string or hash fragment (no library browsing, no upload UI, no styling system).
- Inline `<style>` and inline `<script>` — no separate CSS/JS files, no build step.

This is the entire deliverable if a Tier-1-equivalent single file is all that ships. **Full working code is provided in §2.**

### Tier 2 — Optional Enhancement (nice-to-have, cut first under schedule pressure)

Everything else in this document: Library view, upload panel with progress, toast system, skeleton loaders, empty/error states, pagination, design token system, full accessibility audit. This tier is a **design recommendation only** — Engineering may implement none, some, or all of it, and may choose a different (even simpler) file split than proposed here, as long as Tier 1's acceptance criterion continues to work.

**Recommendation for Engineering:** Ship Tier 1 to unblock acceptance testing immediately; treat Tier 2 as a backlog item, only pursued if the "minimal web player" stretch goal is explicitly prioritized after core API work is complete and verified. **See §0.0 before accepting this backlog item** — it was speculative, not requested.

---

## 2. Tier 1 Deliverable — Minimal Player (Full Code)

This is a complete, self-contained, dependency-free implementation satisfying PRD §4.5 exactly as written. **No separate CSS or JS files are required or implied.**

> **⚠️ File path/name disclaimer (added v3.0 — consistency fix per PM finding #1):** The filename `web/player.html` used below is a **suggested convention only**, matching the informal pattern implied by other Go-project static-asset layouts — it is **not confirmed** by the PRD or any reviewed Architecture document (same caveat as TD-5 and §3.1's Tier 2 structure). The exact path, directory, and serving route **must be confirmed against the actual `main.go` static-file-serving setup** before this filename is treated as final. If Engineering already has a different convention in place (e.g., `static/index.html`, an embedded `//go:embed` asset, a different route prefix), rename/relocate this file accordingly — nothing about the HTML/CSS/JS content below depends on this specific path.

**Notes for Engineering (read before implementing):**
- Video ID resolution order: `?id=` query param first, then `#hash` fragment — supports both a plain link (`player.html?id=abc123`) and a hash-based deep link if this file is later embedded into a larger single-page shell (Tier 2).
- No `Content-Type` is set on the `<source>` tag in Tier 1 — the browser will rely on the server's `Content-Type` response header (confirm `handlers.go` sets this correctly per PRD §4.2; do not assume the client needs to declare it for this tier).
- **`<video>` error handling (moved up from §11 per PM finding #5):** if the ID is malformed, the video doesn't exist, or the stream endpoint returns a non-2xx/non-206 response, the browser fires a native `error` event on the `<video>` element. Tier 1 attaches a minimal listener for this (see code below) so the user sees a plain-text message instead of a silently broken player. This is intentionally not styled/animated — that treatment belongs to Tier 2's `PlayerErrorState` (§3.2.3, §11).
- No auth handling in Tier 1. If `X-API-Key` auth is enabled server-side, this file **will not work as-is** — this is tracked as **OQ-1** in §14 and requires explicit disposition from the Software Architect/Backend Engineer before combining these two stretch features.
- This single file satisfies the acceptance criterion in PRD §13 (manual seek test) with no further dependencies.

## web/player.html

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Video Player</title>
  <style>
    html, body {
      margin: 0;
      padding: 0;
      background: #0b0d10;
      color: #f2f3f5;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    }
    .wrap {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 0.75rem;
      padding: 2rem 1rem;
      max-width: 960px;
      margin: 0 auto;
    }
    video {
      width: 100%;
      background: #000;
      border-radius: 8px;
    }
    p.hint {
      color: #9aa2ad;
      font-size: 0.875rem;
      text-align: center;
      margin: 0;
    }
    p.hint.is-error {
      color: #ef4444;
    }
    code {
      background: #1c2027;
      padding: 0.1rem 0.35rem;
      border-radius: 4px;
      font-size: 0.85em;
    }
  </style>
</head>
<body>
  <div class="wrap">
    <video id="player" controls preload="metadata" playsinline>
      Your browser does not support the video tag.
    </video>
    <p class="hint" id="hint"></p>
  </div>

  <script>
    (function () {
      var params = new URLSearchParams(window.location.search);
      var id = params.get('id') || window.location.hash.replace(/^#/, '');
      var hint = document.getElementById('hint');
      var video = document.getElementById('player');

      if (!id) {
        hint.innerHTML = 'No video ID provided. Append <code>?id={video-id}</code> to this URL.';
        return;
      }

      // Minimal native error handling (PM finding #5, v3.0):
      // covers malformed IDs, 404s, and non-2xx/206 responses from the
      // stream endpoint. Intentionally unstyled — richer error UI is a
      // Tier 2 concern (see §3.2.3 PlayerErrorState).
      video.addEventListener('error', function () {
        hint.textContent = 'Unable to load video "' + id + '". It may not exist, or the server returned an error.';
        hint.classList.add('is-error');
      });

      var source = document.createElement('source');
      // NOTE: stream endpoint path assumes PRD-defined route `/videos/{id}/stream`.
      source.src = '/videos/' + encodeURIComponent(id) + '/stream';
      video.appendChild(source);
      hint.textContent = 'Streaming video ' + id;
    })();
  </script>
</body>
</html>
```

---

## 3. Tier 2 — Optional Enhancement Design (Design Recommendation, Not a Requirement)

Everything below this point describes a **richer experience** that Engineering may build on top of Tier 1, entirely at its discretion. It is offered as a coherent, implementable design system, not a mandate. **Per §0.0, this entire tier was produced speculatively and was not requested by the PRD** — nothing below should block or delay shipping Tier 1, and the swarm should feel free to discard it wholesale.

### 3.1 Recommended (not required) file structure

If Engineering elects to build Tier 2, this is a **suggested** split — not inherited from any PRD or Architecture requirement, and subject to the same path-confirmation caveat now applied consistently to Tier 1 (§2):

```
web/
├── player.html        # Single-page shell: Library view + Player view (client-side toggled)
├── style.css           # Design tokens + component styles
└── app.js              # Fetch calls, view state, render logic (vanilla JS, no deps)
```

Two logical "views" would be implemented as toggled sections within one HTML document, using hash-based navigation (no server-side routing beyond serving the one static file):

| View | Route (client-side, hash-based) | Purpose |
|---|---|---|
| **Library View** | `#/` (default) | List all videos, upload new video |
| **Player View** | `#/videos/{id}` | Stream a single video with native controls |

**Serving mechanism** (e.g., Go `http.FileServer`, exact directory path) is **not confirmed** by any reviewed source document — same caveat as Tier 1's path (§2). Whatever mechanism Engineering already uses to serve Tier 1 is assumed sufficient for Tier 2's additional static files — no new serving infrastructure is implied by this design.

---

### 3.2 Page Layouts (Wireframe-Level Detail)

#### 3.2.1 Global App Shell

```
┌──────────────────────────────────────────────────────────────┐
│  Header Bar                                                    │
│  ┌────────────┐                                    ┌────────┐ │
│  │ ▶ StreamApp │                                    │ (empty)│ │
│  └────────────┘                                    └────────┘ │
├──────────────────────────────────────────────────────────────┤
│                                                                  │
│                     < Active View Renders Here >                │
│                                                                  │
├──────────────────────────────────────────────────────────────┤
│  Footer (muted, single line): "Self-hosted video streaming"    │
└──────────────────────────────────────────────────────────────┘
```

- Fixed-width content column, centered, `max-width: 960px`, generous side gutters on mobile.
- No sidebar, no nav menu — flat IA, only two views.

#### 3.2.2 Library View (`#/`)

```
┌──────────────────────────────────────────────────────────────┐
│  Your Videos                                    [ ⬆ Upload ]   │
│  ───────────────────────────────────────────────────────────   │
│                                                                  │
│  ┌ Upload Panel (collapsed by default; expands on click) ────┐ │
│  │  [ Choose file... ]  [ Upload ]                            │ │
│  │  ▓▓▓▓▓▓▓▓▓░░░░░░░░  62%   (progress bar, shown while active)│ │
│  └────────────────────────────────────────────────────────────┘ │
│                                                                  │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐        │
│  │  ▶        │  │  ▶        │  │  ▶        │  │  ▶        │      │
│  │ [thumb]   │  │ [thumb]   │  │ [thumb]   │  │ [thumb]   │      │
│  │           │  │           │  │           │  │           │      │
│  │ file.mp4  │  │ demo.mov  │  │ clip.webm │  │ intro.mp4 │      │
│  │ 12.4 MB   │  │ 340.1 MB  │  │ 8.2 MB    │  │ 55.0 MB   │      │
│  │ 2h ago    │  │ 1d ago    │  │ 3d ago    │  │ 5d ago    │      │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘        │
│                                                                  │
│                     [ Load more ▾ ]  (pagination — see §5.2)    │
└──────────────────────────────────────────────────────────────┘
```

**Grid behavior:**
- CSS Grid, `repeat(auto-fill, minmax(220px, 1fr))`, `gap: var(--space-4)`.
- Cards are the sole navigation affordance into Player View.
- Thumbnail: since v1 has no transcoding/thumbnail generation (per PRD non-goals), use a static placeholder graphic (centered play-glyph over a neutral gradient tile, content-type badge in corner — mapping table in §9.3).

**Empty state:**
```
┌──────────────────────────────────────────────────────────────┐
│                         🎬                                     │
│                No videos yet                                   │
│         Upload your first video to get started.                │
│                    [ ⬆ Upload a video ]                         │
└──────────────────────────────────────────────────────────────┘
```

**Error state:**
```
┌──────────────────────────────────────────────────────────────┐
│   ⚠  Couldn't load videos.                                     │
│   The service may be starting up or unavailable.                │
│                        [ Retry ]                                │
└──────────────────────────────────────────────────────────────┘
```

**Loading state:** 4 skeleton cards (pulsing gray blocks) on initial fetch.

#### 3.2.3 Player View (`#/videos/{id}`)

```
┌──────────────────────────────────────────────────────────────┐
│  ← Back to library                                              │
│                                                                  │
│  ┌────────────────────────────────────────────────────────┐   │
│  │                 <video> — native controls                 │   │
│  │              (16:9 container, black letterbox)             │   │
│  └────────────────────────────────────────────────────────┘   │
│                                                                  │
│  demo-final-v3.mp4                                              │
│  video/mp4  ·  340.1 MB  ·  Uploaded Jan 14, 2025, 3:02 PM       │
│                                                                  │
│  [ Copy stream link ]   [ Open in new tab ]                     │
└──────────────────────────────────────────────────────────────┘
```

**Player behavior:**
- `<video controls preload="metadata" playsinline>` with `<source src=".../stream" type="{content_type}">`.
- Native browser controls only — no custom scrubber (avoids re-implementing range-seek UX; correctly exercises the API's `206`/`Content-Range` support per PRD §4.2 / §13 acceptance test).
- `Content-Type` from the metadata response is set on `<source>` to aid codec negotiation.
- **Auth handling:** tracked as **OQ-1** (§14) — this is a required Engineering/Architect decision, not resolved by this spec alone.

**Error state (404):**
```
┌──────────────────────────────────────────────────────────────┐
│  ← Back to library                                              │
│                         ⚠                                      │
│                Video not found                                 │
│        It may have been removed, or the link is incorrect.      │
│                    [ Back to library ]                          │
└──────────────────────────────────────────────────────────────┘
```

**Loading state:** dark skeleton block with centered spinner until `GET /videos/{id}` resolves; `<video>` only mounts once metadata succeeds (avoids broken-source flash).

#### 3.2.4 Upload Panel (embedded, Library View)

```
Collapsed:  [ ⬆ Upload video ]

Expanded:
┌────────────────────────────────────────────────────────────┐
│  Upload a video                                    [ ✕ ]     │
│  ┌──────────────────────────────────────────────────────┐   │
│  │   📁  Drag & drop a file here, or [ Browse files ]     │   │
│  │       Accepted: MP4, WebM, MOV · Max 500 MB            │   │
│  └──────────────────────────────────────────────────────┘   │
│  (once file selected)                                         │
│  🎞  demo.mp4  ·  340.1 MB                          [ Upload ] │
│  ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓░░░░░░░░░░░░░░  58%                        │
└────────────────────────────────────────────────────────────┘
```

- Uses `XMLHttpRequest` (not `fetch`) to get `upload.onprogress` events — implementation snippet in §4.2.
- Client-side pre-validation mirrors server rules (not a security boundary) — see §9.1 for the config drift caveat.
- On `201`: panel collapses, success toast appears, Library grid prepends the new card optimistically.
- On `4xx/5xx`: inline error text within the panel (actionable, correctable state).

---

## 4. Tier 2 Implementation Notes (Illustrative Snippets)

These are **implementation guidance snippets**, not full production code — provided so Frontend Engineering has an unambiguous reference for two easily-mis-implemented details.

## web/app.js

```js
// --- Config constants (Tier 2) ---
// KNOWN DRIFT RISK (tracked in tech-debt register, §9.1):
// this value is a static guess matching a commonly-deployed default and
// MUST be reconciled with the server's actual configured max upload size.
// Do not treat this as a security boundary — server-side validation is authoritative.
const MAX_UPLOAD_SIZE_MB = 500;

const ACCEPTED_CONTENT_TYPES = ['video/mp4', 'video/webm', 'video/quicktime'];

// Content-type -> display badge label mapping (tracked in §9.3).
// Any content type accepted by the server but not listed here should
// fall back to a generic "VIDEO" badge rather than throwing.
const CONTENT_TYPE_BADGE_MAP = {
  'video/mp4': 'MP4',
  'video/webm': 'WEBM',
  'video/quicktime': 'MOV',
};

function badgeForContentType(contentType) {
  return CONTENT_TYPE_BADGE_MAP[contentType] || 'VIDEO';
}

// --- Upload with progress (XHR required — fetch upload-progress support
// is inconsistent across browsers as of this writing) ---
function uploadVideo(file, { onProgress, onSuccess, onError }) {
  const xhr = new XMLHttpRequest();
  const formData = new FormData();
  formData.append('file', file);

  xhr.upload.addEventListener('progress', (e) => {
    if (e.lengthComputable) {
      onProgress(Math.round((e.loaded / e.total) * 100));
    }
  });

  xhr.addEventListener('load', () => {
    if (xhr.status >= 200 && xhr.status < 300) {
      onSuccess(JSON.parse(xhr.responseText));
    } else {
      onError(xhr.status, xhr.responseText);
    }
  });

  xhr.addEventListener('error', () => onError(0, 'Network error'));

  xhr.open('POST', '/videos');
  xhr.send(formData);
}

// --- Pagination fetch logic (Tier 2) ---
// PENDING API CONFIRMATION (tracked as OQ-2, §14). Both variants below are
// stubbed so whichever contract Engineering confirms can be enabled by
// deleting the other branch — no redesign required either way.
function buildVideosQuery(paginationState) {
  if (paginationState.mode === 'cursor') {
    return paginationState.nextCursor
      ? `?cursor=${encodeURIComponent(paginationState.nextCursor)}`
      : '';
  }
  if (paginationState.mode === 'offset') {
    return `?limit=${paginationState.limit}&offset=${paginationState.offset}`;
  }
  throw new Error('Unresolved pagination mode — see OQ-2 in design spec §14.');
}
```

## web/style.css

```css
:root {
  /* ---- Color: Neutral scale ---- */
  --color-bg: #0b0d10;
  --color-surface: #14171c;
  --color-surface-raised: #1c2027;
  --color-border: #262b33;
  --color-border-strong: #3a4049;

  --color-text-primary: #f2f3f5;
  --color-text-secondary: #9aa2ad;
  --color-text-muted: #5f6773;

  /* ---- Color: Brand / Accent ---- */
  --color-accent: #5b8cff;
  --color-accent-hover: #7aa0ff;
  --color-accent-text: #0b0d10;

  /* ---- Color: Semantic ---- */
  --color-success: #3ecf8e;
  --color-warning: #f5a623;
  --color-danger: #ef4444;
  --color-danger-bg: rgba(239, 68, 68, 0.12);

  /* ---- Typography ---- */
  --font-family-base: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto,
                      Helvetica, Arial, sans-serif;
  --font-family-mono: "SF Mono", ui-monospace, Menlo, Consolas, monospace;

  --font-size-xs: 0.75rem;
  --font-size-sm: 0.875rem;
  --font-size-base: 1rem;
  --font-size-lg: 1.25rem;
  --font-size-xl: 1.75rem;

  --font-weight-regular: 400;
  --font-weight-medium: 500;
  --font-weight-bold: 700;

  --line-height-tight: 1.25;
  --line-height-base: 1.5;

  /* ---- Spacing scale (4px base unit) ---- */
  --space-1: 0.25rem;
  --space-2: 0.5rem;
  --space-3: 0.75rem;
  --space-4: 1rem;
  --space-5: 1.5rem;
  --space-6: 2rem;
  --space-7: 3rem;

  /* ---- Radius ---- */
  --radius-sm: 4px;
  --radius-md: 8px;
  --radius-lg: 12px;
  --radius-full: 999px;

  /* ---- Shadow ---- */
  --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.4);
  --shadow-md: 0 4px 12px rgba(0, 0, 0, 0.35);
  --shadow-focus: 0 0 0 3px rgba(91, 140, 255, 0.45);

  /* ---- Motion ---- */
  --duration-fast: 120ms;
  --duration-base: 200ms;
  --easing-standard: cubic-bezier(0.4, 0, 0.2, 1);

  /* ---- Layout ---- */
  --content-max-width: 960px;
  --header-height: 56px;

  /* ---- Z-index scale ---- */
  --z-header: 10;
  --z-toast: 100;
}
```

**Rationale:** Dark-first neutral palette (reduces glare competing with video content; minimizes visual noise around placeholder thumbnails). Single accent color used sparingly for primary actions and focus rings, consistent with a minimal-surface design philosophy.

---

## 5. Component Hierarchy & Interaction Contracts (Tier 2)

### 5.1 Component tree

```
App (root, mounted on <body>)
├── AppHeader
│   └── Logo/Title ("▶ StreamApp")
├── Router (hash-based dispatch function, not a framework)
├── LibraryView
│   ├── LibraryHeader (Title + UploadToggleButton)
│   ├── UploadPanel (DropZone, FilePreviewRow, ProgressBar, InlineErrorText)
│   ├── VideoGrid → VideoCard × N (ThumbnailPlaceholder, Filename, MetaLine, click handler)
│   ├── LoadMoreButton (pagination — see §5.2)
│   ├── SkeletonGrid
│   ├── EmptyState
│   └── ErrorState
├── PlayerView
│   ├── BackLink
│   ├── VideoSkeleton
│   ├── VideoStage (<video> + <source>)
│   ├── VideoMetaBlock (FilenameHeading, MetaLine)
│   ├── ActionRow (CopyLinkButton, OpenInNewTabLink)
│   └── PlayerErrorState
├── ToastHost (aria-live region, global)
└── AppFooter
```

Each "component" is a plain JS function returning/mutating DOM (`function renderVideoCard(video) { ... }`) — no framework, no virtual DOM, no JSX, no bundler.

### 5.2 ⚠️ Pagination Contract — Escalated as Open Question (OQ-2)

The `LoadMoreButton` behavior depends on a backend decision not yet confirmed:

| If API returns... | UI behavior |
|---|---|
| `next_cursor` field (cursor-based) | `LoadMoreButton` sends `?cursor={next_cursor}` on click; hidden when `next_cursor` is absent/null. |
| `limit` / `offset` only | `LoadMoreButton` tracks a local `offset` counter, increments by `limit` per page, sends `?limit=X&offset=Y`; hidden when the last page returns fewer than `limit` items. |

**Escalation note (added v3.0 — PM finding #4):** PRD §4.3 genuinely leaves this open at the requirements level — it is not something the Designer or Frontend Engineer should resolve unilaterally, since it affects the API contract itself, not just the UI. This has been logged as **OQ-2** in the cross-team **§14 Open Questions Tracker** for a PM/Architect decision, rather than being closed out silently in implementation later. Both UI variants remain specified here so no redesign is needed once OQ-2 is resolved — only the fetch/param logic in `app.js` differs.

---

## 6. ⚠️ API Key Auth vs. `<video>` Player Compatibility — Routed to Architect/Backend Engineer (OQ-1)

**This is a new backend requirement being surfaced by this design, not a resolved detail, and not a decision for the UI/UX Designer agent to make.**

If the optional API-key auth stretch feature (header-based, e.g. `X-API-Key`) is implemented **and** this HTML player (Tier 1 or Tier 2) is also implemented, there is a compatibility gap:

- Native `<video>`/`<source>` elements **cannot send custom HTTP headers**. They can only request via plain GET with cookies or query parameters.
- If `X-API-Key` header validation is the *only* auth path on `/videos/{id}/stream`, the `<video>` element will receive a `401`/`403` and **fail to play**, silently breaking the stretch player feature.

**Options for disposition (not decided by this spec):**
1. Add a query-parameter fallback (e.g., `?api_key=...`) accepted by the stream endpoint specifically, in addition to the header. **This is new backend behavior and must be explicitly approved and implemented by Engineering** — it is not implied by PRD §4.5 or any existing handler.
2. Leave API-key auth and the HTML player **mutually exclusive** in v1: document that the stretch UI is only supported when no API key is configured (simplest, zero new backend work, but limits the player's usefulness in secured deployments).
3. Skip the HTML player entirely if API-key auth is enabled for a given deployment.

**Design recommendation (non-binding, offered for context only):** Option 2 for the fastest path to shipping both stretch features independently without new backend surface area, with Option 1 as a documented future enhancement. If Option 1 is chosen, the key must never be persisted (in-memory/`sessionStorage` only, never a cookie), never rendered into visible DOM text, and "Copy stream link" should only include the key if an operator has explicitly enabled a "share link" toggle.

**Routing (added v3.0 — PM finding #3):** This section prescribes backend/security behavior, which exceeds the UI/UX Designer's mandate to decide unilaterally. It is formally **routed to the Software Architect / Backend Engineer agent** for disposition and is tracked as **OQ-1** in §14, rather than being left as a standalone open item inside a design document. The UI/UX Designer's role here is limited to (a) surfacing the incompatibility, which would otherwise silently break a shipped feature, and (b) describing the UI-facing implications of whichever option is chosen — not selecting the backend approach.

---

## 7. Design Tokens Usage & Key Component Styling (Tier 2)

### 7.1 Buttons
| Variant | Background | Text | Border | Use |
|---|---|---|---|---|
| Primary | `--color-accent` | `--color-accent-text` | none | Upload, Retry, primary CTAs |
| Secondary | transparent | `--color-text-primary` | `1px solid --color-border-strong` | Back, Open in new tab |
| Danger-adjacent | transparent | `--color-danger` | `1px solid --color-danger` | Reserved; no destructive actions in v1 UI |

- `border-radius: var(--radius-md)`, `padding: var(--space-2) var(--space-4)`, `font-weight: var(--font-weight-medium)`.
- Focus state: `outline: none; box-shadow: var(--shadow-focus);` — never remove focus indication.
- Disabled state: `opacity: 0.5; cursor: not-allowed;`.

### 7.2 Video Card
- Container: `background: var(--color-surface); border: 1px solid var(--color-border); border-radius: var(--radius-lg); overflow: hidden; transition: transform var(--duration-base) var(--easing-standard), box-shadow var(--duration-base);`
- Hover/focus: `transform: translateY(-2px); box-shadow: var(--shadow-md); border-color: var(--color-border-strong);` — entire card is a real `<button>`/anchor for keyboard access.
- Thumbnail area: `aspect-ratio: 16/9; background: linear-gradient(135deg, #1c2027, #262b33); display:flex; align-items:center; justify-content:center;` with centered SVG play-triangle at 40% opacity and a top-right badge (mapping in §9.3).
- Text block padding: `var(--space-3) var(--space-4)`. Filename truncates with ellipsis; full name in `title` attribute.
- Meta line: `font-size: var(--font-size-xs); color: var(--color-text-secondary);`.

### 7.3 Video Stage (Player View)
- `background: #000; border-radius: var(--radius-lg); overflow: hidden; aspect-ratio: 16/9; width: 100%;`
- `<video>`: `width: 100%; height: 100%; display: block;`
- `box-shadow: var(--shadow-md);` to lift off the dark page background.

### 7.4 Progress Bar (Upload)
- Track: `height: 6px; background: var(--color-border); border-radius: var(--radius-full); overflow: hidden;`
- Fill: `background: var(--color-accent); transition: width var(--duration-fast) linear;`
- Percentage label as adjacent text, not overlaid on the bar.

### 7.5 Toast
- `position: fixed; bottom: var(--space-5); z-index: var(--z-toast);` (bottom-center mobile / bottom-right desktop)
- `background: var(--color-surface-raised); border: 1px solid var(--color-border-strong); border-radius: var(--radius-md); box-shadow: var(--shadow-md); padding: var(--space-3) var(--space-4);`
- Success: left border accent `3px solid var(--color-success)`, `role="status" aria-live="polite"`, auto-dismiss 4s.
- Error: `3px solid var(--color-danger)`, `role="alert" aria-live="assertive"`, manual dismiss only.

### 7.6 Skeleton Loaders
- `background: linear-gradient(90deg, var(--color-surface) 25%, var(--color-surface-raised) 37%, var(--color-surface) 63%); background-size: 400% 100%; animation: skeleton-pulse 1.4s ease-in-out infinite;`
- Matches the real card's box model exactly to prevent layout shift.

---

## 8. Responsive Breakpoints (Tier 2)

| Breakpoint | Width | Behavior |
|---|---|---|
| `--bp-sm` | < 480px | Grid collapses to 1 column; header title shrinks to icon-only; upload panel drop-zone stacks vertically. |
| `--bp-md` | 480–768px | Grid: 2 columns. |
| `--bp-lg` | 768–1024px | Grid: 3 columns. |
| `--bp-xl` | > 1024px | Grid: 4 columns (capped — content column capped at `--content-max-width: 960px`). |

Implemented via a single `grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));` — the table above is the effective resulting behavior, not manual `@media` column-count rules. One `@media (max-width: 480px)` block handles header/upload-panel stacking only.

---

## 9. Tracked Tech-Debt Register

Formal tracking of known limitations flagged during design, so they are not lost as inline footnotes:

| ID | Item | Description | Owner / Action |
|---|---|---|---|
| **TD-1** | `MAX_UPLOAD_SIZE_MB` drift | Client-side constant (`500`) is a static guess, not sourced live from server config. If server env var changes without redeploying the static asset, client-side pre-validation will give incorrect feedback (server-side validation remains authoritative and safe either way). | Engineering to decide: hardcode-and-document (current plan) vs. fetch from a lightweight config endpoint. |
| **TD-2** | API-key + `<video>` incompatibility | See §6. Native `<video>` cannot send custom headers; a query-param fallback is a new, unapproved backend surface. | **Escalated as OQ-1 (§14)** — requires Software Architect/Backend Engineer sign-off before either stretch feature assumes the other works. |
| **TD-3** | Pagination contract undetermined | Cursor vs. limit/offset not confirmed (PRD §4.3 leaves this open). See §5.2 for both UI variants. | **Escalated as OQ-2 (§14)** — Engineering/PM to confirm `GET /videos` response shape; `app.js` fetch logic to be written against confirmed contract. |
| **TD-4** | Content-type → badge label mapping | `video/quicktime` → "MOV" and similar mappings must live in `app.js`, not be inferred ad hoc. | Mapping table now explicit in §4 (`CONTENT_TYPE_BADGE_MAP`); any new accepted content type must be added there or falls back to a generic "VIDEO" badge. |
| **TD-5** | File/serving path assumptions (both tiers) | No confirmed file path or serving mechanism (`http.FileServer` or otherwise) exists in reviewed source documents, for **either Tier 1 (§2) or Tier 2 (§3.1)** structures. | Whatever mechanism Engineering already uses to serve static assets is assumed sufficient; no new serving infra implied by either tier. |

---

## 10. Accessibility Notes (Applies to Tier 1 and Tier 2)

- All interactive elements (`VideoCard`, buttons) are real `<button>` or `<a>` elements — never bare `<div onclick>`.
- Video cards (Tier 2): `aria-label="Play {filename}, {size}, uploaded {relative time}"`.
- Focus-visible ring (`--shadow-focus`) applied via `:focus-visible`, not `:focus`.
- Color contrast: body text on background exceeds WCAG AA (>7:1); secondary text on surface verified ≥ 4.5:1.
- `<video>` element (both tiers): native controls only, inheriting browser/OS accessible media-control semantics. Captioning/subtitles out of scope per PRD non-goals (no transcoding pipeline in v1).
- Toasts (Tier 2) use `aria-live` regions so screen readers announce upload results without requiring focus movement.
- Upload drop-zone (Tier 2) includes a visually-hidden native `<input type="file">` as the actual interactive control — drag-and-drop is purely an enhancement layer.

---

## 11. States Summary Matrix (Tier 2)

| View | Loading | Empty | Error | Success |
|---|---|---|---|---|
| Library | Skeleton grid (4 cards) | Empty-state illustration + CTA | Error banner + Retry button | Populated grid, paginated per §5.2 |
| Upload | Progress bar + disabled inputs | n/a | Inline panel error text | Collapse panel + success toast + optimistic card insert |
| Player | Dark skeleton stage + spinner | n/a (video either exists or 404s) | 404 illustration + Back-to-library CTA | Mounted `<video>` with native controls |

*(Tier 1 has no library/upload states — only Player-equivalent loading/error/success. The error case is handled by the browser's own `<video>` `error` event, with a minimal plain-text listener now included directly in §2's code sample, not deferred to this table alone.)*

---

## 12. Explicit Non-Requirements (Keeping Scope Honest)

- **No** custom video scrubber/controls (native `<video controls>` only) — either tier.
- **No** thumbnail generation/extraction (static placeholder only, Tier 2) — no transcoding per PRD non-goals.
- **No** client-side routing library, CSS framework, or bundler/build step — either tier.
- **No** authentication UI (login form). The optional API-key model, if implemented, remains a dev-facing header/config affordance, not a user-facing auth system — matches PRD §1.4 non-goal (no auth system). See §6/OQ-1 for the compatibility caveat this introduces.
- **No** dark/light theme toggle (Tier 2) — single dark theme by design decision.
- **No** multi-file structure requirement for Tier 1 — it ships as one file, matching PRD §4.5's literal wording (filename itself unconfirmed — see §2 disclaimer).

---

## 13.
