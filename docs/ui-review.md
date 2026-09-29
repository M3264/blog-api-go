# Account-based site UI review — 2026-09-29

## Scope and visual continuity

Reviewed the Go template `internal/httpapi/web/site.html`, `internal/httpapi/web/site.css` and the Vite TypeScript source `ui/site.ts`. The publication, account screens and editorial desk extend the established Offscript identity: white surfaces, DM Sans, violet accents, the lowercase masthead, rounded photography and fine separators. `DESIGN.md` is preserved. This work does not introduce a replacement visual world or regenerate the design-system files.

The incumbent document describes an archive/topic rail and violet dot navigation that do not precisely match the current grid and navigation. This is existing documentation drift, recorded here without changing the design authority or introducing unrelated visual repairs.

The current [Vercel Web Interface Guidelines](https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md) were fetched for this review. Checked native navigation and form semantics, labels and autocomplete, keyboard focus, feedback states, touch controls, long-content wrapping and responsive reading order. This is a focused UI review, not an accessibility certification or security penetration test.

## Review corrections

The finish reviewer identified four material issues, addressed together:

- Mobile editorial layout now places title, summary, story and save/publish actions before metadata and revision history.
- The native account menu now has a visible chevron that rotates when expanded.
- The Tiptap content surface has a visible keyboard focus outline and its surrounding border responds to focus.
- Formatting buttons expose the active selection through `aria-pressed` and visible active styling; undo availability updates with the editor state.

The final reviewer disposition is **ship at fix-list scope**: all four material fixes are resolved. Recaptured editor evidence shows the desktop keyboard-focus violet ring and active Bold state, plus disabled Undo on mobile; the corresponding browser assertions passed. This scoped verdict closes the correction list above.

## Browser evidence

Guest, reader and admin journeys passed in Chromium at 1440 × 1000 and 390 × 844, with no horizontal overflow or JavaScript errors reported. Accounts and content were fictional and the preview used an isolated PostgreSQL schema. No live users or secrets were used.

Final full-page evidence is under `.impeccable/review/`:

| Surface | Desktop | Mobile |
| --- | --- | --- |
| Publication | `desktop.png` | `mobile.png` |
| Article | `desktop-article.png` | `mobile-article.png` |
| Sign-in | `desktop-login.png` | `mobile-login.png` |
| Account settings | `desktop-settings.png` | `mobile-settings.png` |
| Editorial desk | `desktop-editor.png` | `mobile-editor.png` |

The documentation pass directly inspected the publication desktop and editor mobile captures and compared the template, CSS and formatting-state source to the incumbent identity. The remaining capture set and journey results form the build/reviewer evidence.

Impeccable's mechanical detector could not resolve Go's embedded `/assets/site.css` mapping and falsely inferred an unstyled, flat hierarchy. The delivered CSS provides a 16px body, a 64px desktop primary headline, a 38px heading minimum and a 36px general h1 override below 400px and a 28px base secondary heading; article and editorial contexts use their own larger heading roles. Browser screenshots show the actual styled hierarchy.

## Component provenance and limits

21st.dev account-menu search succeeded. Origin UI component retrieval returned `locked: true`, so no retrieved source was used. The grouped native `<details>` account menu is an original concept adaptation using the existing Offscript styles; see `docs/ui-components.md` for provenance.

The sender and initial admin email are now configured privately, and host-local SMTP signing has been validated. Resend delivery and the administrator’s real Google/password login have since been verified, and the account-based site is now live. This review establishes the local interface and fictional browser journeys; it does not establish real provider delivery, real Google authentication or a production rollout. See `docs/rollout.md` for the deployment checks.

---

# Historical publication-only review — 2026-09-29

The following notes describe the earlier `index.html` / `styles.css` / `app.js` surface. Their paths, measurements and dependency claims apply to that earlier implementation, not the account-based site above.

Reviewed against Vercel Web Interface Guidelines:
https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md

## internal/httpapi/web/index.html
- Fixed navigation buttons to native links, added a skip link, explicit input names/autocomplete and a theme color.
- Preserved labels, semantic landmarks, native validation and live status announcements.

## internal/httpapi/web/styles.css
- Fixed reduced-motion handling, explicit transition properties, input placeholder contrast, long-content wrapping and mobile control sizing.
- Preserved visible focus and the new Offscript white/violet publication identity.
- Sentence-case headings deliberately retain the publication's existing voice.

## internal/httpapi/web/app.js
- Added image dimensions/decoding, lead-image priority, URL filter state, stale-response protection and unsaved-edit guards.
- Added inline save feedback and disabled duplicate save actions during requests.
- Content remains textContent-based; no HTML injection or new external runtime dependencies.

## Browser verification
Chromium, 1440px and 390px: homepage and full article screenshots inspected. No horizontal overflow and no JavaScript errors. Article navigation, search, filter reload and unsaved-edit cancellation passed. Editor inspected at 390px.

Impeccable's mechanical detector could not resolve Go's /playground/styles.css mapping and reported a flat type hierarchy from unstyled HTML. Actual computed homepage h1 sizes were 62px desktop and 40px mobile, with 15px base text; the visual review confirms the hierarchy.

This is a focused UI review, not an accessibility certification or security penetration test.

## Follow-up visual correction
Removed the promotional hero and color-block lead. Added a story-led front page, locally hosted Newsreader/DM Sans and three illustrative sample-story photographs. Rechecked desktop/mobile screenshots, full article routes, search reload, dirty-editor cancellation and JavaScript errors. Both 1440px and 390px render without horizontal overflow; all checks passed. Article and editor behavior is preserved.
