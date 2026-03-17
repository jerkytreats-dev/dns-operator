# Compatibility Policy

Date: 2026-03-16
Status: active

## Intent

This project prioritizes product clarity and ownership over preserving every legacy behavior from the reference implementation.

## Rules

- Backward incompatible changes are allowed when they improve the operator's product model or ownership boundaries.
- Favor the outcome-first architecture centered on `PublishedService`, authoritative internal DNS, shared SAN certificate management, and split-DNS repair.
- Any backward incompatible change must be called out to the user before commit.
- Commit messages must reflect breaking change severity using conventional commit rules.
- Use `type!:` or `type(scope)!:` for breaking changes.
- Add a `BREAKING CHANGE:` footer with a concise migration impact note.
- Keep user-facing migration impact explicit in review notes, release notes, and cutover docs.
