# Repository guidelines

This is a public open-source repository. Treat tracked files, commit messages,
PR descriptions, comments, CI logs and release artifacts as public content.

- Never publish credentials, private addresses or hostnames, infrastructure
  inventories, deployment details, operational logs, backup locations or internal
  review notes. Do not copy private test results into public documentation.
- Use synthetic fixtures, example domains and obvious credential placeholders.
  Keep public validation summaries limited to reproducible automated checks.
- Keep README, changelog and PR descriptions concise. Document supported behavior,
  installation, configuration, compatibility limits and upgrade requirements.
  Omit work diaries, incident narratives and unsupported compatibility claims.
- Before pushing, inspect the full staged diff and outgoing commits for private
  information. Check generated artifacts and PR text as well as source files.
  Run a secret scanner when available; it does not replace manual review.
- Preserve regression tests and release gates. Run checks appropriate to the
  change; do not alter application behavior during documentation-only work.
- Do not publish deployment-specific information without explicit approval of
  the exact content. Keep production operations separate from public reporting.
- If private information was published, remove it from current content and
  reachable history, inspect artifacts and edit history, and state any remaining
  exposure. Closing a PR or deleting a branch does not erase GitHub caches.
