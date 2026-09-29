# SMTP deployment

The sender is editable through `BLOG_EMAIL_FROM`, defaulting in the example to `blog@kennyy.tech`. The administrator recipient is a separate private `BLOG_INITIAL_ADMIN_EMAIL`. Mail is queued durably in PostgreSQL; the worker hands it to SMTP and retries rejected submissions. Relay acceptance does not establish inbox placement.

## Host service

This server has Postfix listening on **127.0.0.1:25 only** and OpenDKIM on **127.0.0.1:8891 only**. Those settings supported local testing. The user has since selected Resend; the private environment now sets `RESEND_API_KEY` and leaves `BLOG_SMTP_ADDR` empty, so the app uses HTTPS delivery through Resend. Postfix/OpenDKIM remain installed but are not selected by the app. Mail from the sending domain is signed with selector `offscript20260929`; the private signing key stays under `/etc/opendkim/keys/` with owner-only access. Missing signer causes a temporary submission failure. The account-based Go service is now the public deployment, using Resend over HTTPS.

A Docker container's loopback belongs to that container. These host-local settings require running Go as the host service. For a container deployment, configure an accessible authenticated relay with verified TLS, or provision a dedicated private relay network and restrict its clients before changing the app transport. Do not expose the host's unauthenticated relay to the internet.

## DNS and network completion

Publish a DNS-only A record for `mail.kennyy.tech` pointing to the server's public IPv4. The A and MX records are now publicly visible and correct. Ask the hosting provider to set reverse DNS for that IPv4 to `mail.kennyy.tech`. Publish SPF allowing that IPv4, DKIM using the public TXT file at `/etc/opendkim/keys/kennyy.tech/offscript20260929.txt`, and an initial DMARC record (`v=DMARC1; p=none`) at `_dmarc.kennyy.tech`. Merge SPF with any existing authorized senders rather than adding multiple SPF records. Do not proxy the mail hostname through Cloudflare.

A private copy of the exact public DNS values is saved in `backups/smtp-dns.txt` on this host. It contains no private signing key. Direct SMTP to two Gmail MX hosts on port 25 timed out during setup; the hosting/network administrator must enable outbound port 25 or provide an authenticated upstream relay on port 587. Postfix can relay through that upstream while remaining the app's local SMTP endpoint.

Public DNS verification confirmed the A, MX, SPF and DMARC records, and the DKIM TXT matches the configured key (`opendkim-testkey`: key OK). Reverse DNS/PTR is still absent and outbound SMTP port 25 still times out; port 587 connects. After resolving connectivity and PTR, check `opendkim-testkey -d kennyy.tech -s offscript20260929 -vvv`, Postfix queue/logs, and an actual account verification/reset delivery before launch. [Gmail sender requirements](https://support.google.com/mail/answer/81126) describe authentication, forward/reverse DNS and TLS expectations.

## Transport options

`BLOG_SMTP_MODE=starttls` requires advertised STARTTLS and validates the server certificate. `tls` uses implicit TLS. `local` permits plaintext only on loopback. Remote authentication uses private `BLOG_SMTP_USER` and `BLOG_SMTP_PASSWORD`. The sender, SMTP credentials and Google secret stay outside Git. SMTP takes precedence when configured; clear `BLOG_SMTP_ADDR` to select Resend instead.

## Current Resend configuration

Resend reports `kennyy.tech` verified, including its DKIM and SPF records. The sender remains `blog@kennyy.tech`. A setup test sent to the user-supplied admin recipient was accepted and subsequently reported `delivered` by Resend on 2026-09-29. This establishes provider delivery to the recipient server; inbox placement was not inspected. The administrator has verified password/Google sign-in and the account-based site is now the public default. Keep Resend records and DMARC; the earlier host-mail DNS records are no longer required by this transport, but have not been removed.
