# Offscript UI component provenance

Installed as a framework-native adaptation (HTML and CSS), preserving the existing Go embed pipeline.

- 21st.dev **Editorial Hero**, demo 19075, by **felipemenezes098**: https://21st.dev/@felipemenezes098/components/hero-05
- Retrieved through the 21st.dev MCP on 2026-09-29. Implemented in `internal/httpapi/web/index.html` (`editorial-hero`) and `styles.css`.
- Adapted the component's linked title, dashed separator and description pattern to semantic HTML and native CSS. No React runtime, demo copy or original imagery is used.

## Offscript redesign
The split promotional hero was removed after visual feedback. The homepage now leads with actual articles, using a large image-led story and two supporting articles. Newsreader and DM Sans are self-hosted under their SIL Open Font Licenses (included in this directory).

Illustrative photographs, downloaded from Unsplash and served locally, apply only to three seeded sample articles when no cover is supplied:
- writing.jpg: https://images.unsplash.com/photo-1455390582262-044cdead277a
- workspace.jpg: https://images.unsplash.com/photo-1499750310107-5fef28a66643
- library.jpg: https://images.unsplash.com/photo-1497633762265-9d179a990aa6

Database content and author-provided covers are unchanged.
