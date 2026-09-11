# Releasing

1. Update `debian/changelog` and the first version in `CHANGELOG.md` together.
   Use a new `MAJOR.MINOR.PATCH` version; before 1.0, contract changes increment
   the minor version. Run `make release-check`.
2. Review the PR and merge after CI passes. On `main`, tests, vulnerability
   scanning, native ZFS checks and amd64/arm64 package builds gate publication.
3. A new version publishes to Cloudsmith and creates a GitHub Release with
   packages, source packaging, changelog notes and `SHA256SUMS`. Later merges
   without a version bump publish only development packages.

Maintainers must configure `CLOUDSMITH_API_KEY`, allow the publish job to create
version tags, and require CI/reviews through repository rules. PRs do not publish.
The publish job alone requests `contents: write`.

CI artifacts are retained for 14 days. On a partial upload failure, verify existing
package checksums and resolve incomplete uploads before rerunning the failed job
with its original artifacts. Never overwrite a stable version or move its tag.
Verify published checksums and test installation/upgrades in staging.
