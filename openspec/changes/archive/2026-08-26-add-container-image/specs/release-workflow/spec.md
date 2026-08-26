## MODIFIED Requirements

### Requirement: Release-please job outputs gate the build job

The release-please job SHALL expose `release_created` and `tag_name` as job outputs. The build-and-upload job and build-and-push-image job SHALL each depend on release-please and SHALL run only when `release_created` is `true`. The two artifact jobs SHALL remain independent and SHALL be eligible to run in parallel.

#### Scenario: Release created triggers both artifact jobs

- **WHEN** release-please creates a new release and `release_created` is `true`
- **THEN** build-and-upload and build-and-push-image SHALL each execute with the `tag_name` output

#### Scenario: No release skips both artifact jobs

- **WHEN** release-please does not create a release and only updates or leaves pending a release pull request
- **THEN** build-and-upload and build-and-push-image SHALL NOT execute

### Requirement: Release workflow permissions are minimal

The release workflow SHALL assign automatic `GITHUB_TOKEN` permissions at the narrowest job scope. The release-please job SHALL declare only `contents: write`, `issues: write`, and `pull-requests: write`; the build-and-upload job SHALL declare only `contents: write`; and the build-and-push-image job SHALL declare only `contents: read` and `packages: write`. The build-and-upload and container publishing job MUST use their built-in `${{ github.token }}` rather than `MY_RELEASE_PLEASE_TOKEN`. No job SHALL declare `id-token: write`, `attestations: write`, or any automatic-token permission unrelated to its work. The explicit PAT supplied only to the release-please action is governed separately by the `Repo secret set via gh CLI` requirement because job-level permissions cannot reduce PAT scopes.

#### Scenario: Release-please automatic-token permissions are scoped

- **WHEN** the release-please job permissions are inspected
- **THEN** they SHALL contain exactly `contents: write`, `issues: write`, and `pull-requests: write`, while only its release-please action SHALL receive `MY_RELEASE_PLEASE_TOKEN`

#### Scenario: Binary and Debian artifact permissions are scoped

- **WHEN** the build-and-upload job permissions and environment are inspected
- **THEN** they SHALL contain exactly `contents: write` and `gh release upload` SHALL authenticate with `${{ github.token }}` rather than `MY_RELEASE_PLEASE_TOKEN`

#### Scenario: Container image permissions are scoped

- **WHEN** the build-and-push-image job permissions and registry login are inspected
- **THEN** it SHALL contain exactly `contents: read` and `packages: write` and SHALL authenticate with `${{ github.token }}`

## ADDED Requirements

### Requirement: Release publishes the official GHCR image

The build-and-push-image job SHALL build the Dockerfile once for `linux/amd64` with VERSION set to the release-please `tag_name` and SHALL push the immutable full `vX.Y.Z` tag to the public ShadowDNS GHCR package using the repository's built-in `GITHUB_TOKEN`. The complete release workflow SHALL use one fixed concurrency group with `cancel-in-progress: false`, preventing another release workflow from publishing a newer GitHub Release between an older run's newest-release check and latest-tag update. The post-build latest update step SHALL query the repository's newest published release and SHALL move `latest` to the just-built exact tag only when that tag is still the newest release. Workflow-level serialization and this guard MUST prevent an older, slower workflow run from moving `latest` backward. After a successful newest-release update, the full tag and `latest` MUST reference the same image digest. The workflow SHALL NOT publish edge, major-only, minor-only, or architecture-suffixed tags.

#### Scenario: Versioned image is published

- **WHEN** release-please creates release `v0.9.0`
- **THEN** build-and-push-image SHALL push `ghcr.io/<repository-owner>/shadowdns:v0.9.0`, update-latest-image SHALL point `ghcr.io/<repository-owner>/shadowdns:latest` at that exact tag only while `v0.9.0` remains the newest release, and both tags SHALL reference one linux/amd64 image digest

#### Scenario: Older release finishes after a newer release

- **WHEN** an older release image finishes after a newer GitHub release has already been published
- **THEN** update-latest-image SHALL detect that its tag is not the repository's newest release and SHALL leave `latest` unchanged

#### Scenario: Image build or push fails

- **WHEN** the Docker build, GHCR authentication, exact tag push, or newest-release latest update fails
- **THEN** the corresponding container publishing job SHALL fail and SHALL NOT hide the failure with `continue-on-error`

#### Scenario: No release leaves image tags unchanged

- **WHEN** a push to `main` does not cause release-please to create a release
- **THEN** the workflow SHALL NOT authenticate to GHCR, build a release image, or modify GHCR tags

### Requirement: Release image is verified before publication

The build-and-push-image job SHALL build the release image into the runner's local container store and SHALL run the same repository-owned image contract verification script the pull request CI job uses, supplying the release tag as the expected version and the release source, revision, and created values as the expected OCI labels. The job SHALL push to GHCR only after that verification passes, so release-only build arguments and metadata cannot reach a published tag unverified.

#### Scenario: Verified release image is published

- **WHEN** the release image passes the shared contract verification
- **THEN** the job SHALL push the exact `vX.Y.Z` tag and only then evaluate the newest-release guard for `latest`

#### Scenario: Release-only contract drifts

- **WHEN** the release image's architecture, identity, entrypoint, default command, exposed ports, health-check absence, version output, or OCI labels differ from the container-image specification
- **THEN** the verification step SHALL fail the job and no image SHALL be pushed to GHCR

### Requirement: Release image carries release metadata

The image job SHALL pass the release tag, source repository URL, commit SHA, and the source commit timestamp in OCI-compatible form to the Docker build. The source commit timestamp SHALL remain stable when the same release commit is rebuilt and SHALL NOT use the workflow job's wall-clock start time. The resulting image SHALL expose these as `org.opencontainers.image.version`, `org.opencontainers.image.source`, `org.opencontainers.image.revision`, and `org.opencontainers.image.created` labels. The image job SHALL NOT generate provenance attestations or an SBOM.

#### Scenario: OCI labels match release inputs

- **WHEN** release `v0.9.0` is built from commit `0123456789abcdef0123456789abcdef01234567`
- **THEN** the published image version label SHALL equal `v0.9.0`, the revision label SHALL equal that complete commit SHA, the source label SHALL identify the ShadowDNS repository, and the created label SHALL equal that commit's stable OCI-formatted timestamp

#### Scenario: Supply-chain extras remain out of scope

- **WHEN** the image job completes
- **THEN** it SHALL NOT request OIDC or attestation permissions and SHALL NOT publish provenance or SBOM artifacts

### Requirement: Container publishing actions are supply-chain pinned

Every third-party GitHub Action newly used by the container build and publish path SHALL be pinned to a full commit SHA with a nearby comment naming the corresponding upstream release tag. Floating action tags SHALL NOT be used.

#### Scenario: Workflow action references are audited

- **WHEN** the container build and publish steps in the workflow are inspected
- **THEN** every third-party `uses` reference introduced by this change SHALL contain a 40-character commit SHA and a human-readable release tag comment
