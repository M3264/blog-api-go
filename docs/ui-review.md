# UI review — 2026-09-29

Reviewed against Vercel Web Interface Guidelines:
https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md

## internal/httpapi/web/index.html
- Fixed navigation buttons to native links, added a skip link, explicit input names/autocomplete and a theme color.
- Preserved labels, semantic landmarks, native validation and live status announcements.

## internal/httpapi/web/styles.css
- Fixed reduced-motion handling, explicit transition properties, input placeholder contrast, long-content wrapping and mobile control sizing.
- Preserved visible focus and the incumbent burgundy/cream editorial identity.
- Sentence-case headings deliberately retain the publication's existing voice.

## internal/httpapi/web/app.js
- Added image dimensions/decoding, lead-image priority, URL filter state, stale-response protection and unsaved-edit guards.
- Added inline save feedback and disabled duplicate save actions during requests.
- Content remains textContent-based; no HTML injection or new external runtime dependencies.

## Browser verification
Chromium, 1440px and 390px: homepage and full article screenshots inspected. No horizontal overflow and no JavaScript errors. Article navigation, search, filter reload and unsaved-edit cancellation passed. Editor inspected at 390px.

Impeccable's mechanical detector could not resolve Go's /playground/styles.css mapping and reported a flat type hierarchy from unstyled HTML. Actual computed homepage h1 sizes were 62px desktop and 40px mobile, with 15px base text; the visual review confirms the hierarchy.

This is a focused UI review, not an accessibility certification or security penetration test.
