# Product
<!-- impeccable:product-schema 1 -->
## Platform
web
## Purpose
Offscript is a publication and community for stories, guides and updates. Guests browse stories, search, topics and authors at permanent article URLs. Reader accounts follow authors and topics, save and like stories, participate in conversations, receive notifications and manage email preferences. Admin accounts manage publication, members, moderation and delivery through an editorial desk with a Tiptap rich-text editor.
## Constraints
Keep the website in the existing repository, using Go HTML templates and a Vite-built TypeScript bundle embedded with its CSS and assets. Preserve the public Go APIs, existing article URLs and slugs, published content, PostgreSQL and Redis. Existing text content remains readable; rich content is sanitized before rendering. Public registration creates readers; editorial administration requires an authorized admin account. No university-specific branding.
## Current task
Extend the established Offscript publication into an account-based blog and community. Preserve its white surfaces, dark DM Sans typography, violet accents, lowercase masthead and image-led stories, guided by It's Nice That and the existing 21st.dev Blog Cards adaptation. Reading, account settings and the editorial desk inherit this visual system; DESIGN.md remains the incumbent visual authority.

The site is live on blog.kennyy.tech using the Go service, PostgreSQL and Redis. Resend delivery and the administrator’s password/Google sign-in are verified. Browser review used fictional accounts in an isolated schema; no fictional accounts were created in production. Private credentials and uploaded media remain outside Git. Deployment and rollback evidence is recorded in docs/rollout.md.
