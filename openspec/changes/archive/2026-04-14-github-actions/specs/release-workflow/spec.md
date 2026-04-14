## ADDED Requirements

### Requirement: Release workflow triggers only on push to main

The release workflow SHALL trigger exclusively on `push` events to the `main` branch. No other events or branches SHALL trigger this workflow.

#### Scenario: Push to main triggers release workflow

- **WHEN** a commit is pushed to the `main` branch (e.g., via merged PR)
- **THEN** the release workflow SHALL execute

#### Scenario: Push to non-main branch does not trigger release

- **WHEN** a commit is pushed to a branch other than `main`
- **THEN** the release workflow SHALL NOT execute

### Requirement: Release-please manages version and changelog

The release workflow SHALL use `googleapis/release-please-action@v4` with `release-type: go` to automatically manage semantic versioning and changelog generation. The action SHALL use `secrets.MY_RELEASE_PLEASE_TOKEN` (a PAT) to authenticate, because the `main` branch has branch protection rules.

#### Scenario: Conventional commit triggers release PR

- **WHEN** a conventional commit (e.g., `feat:`, `fix:`) is pushed to `main`
- **THEN** release-please SHALL create or update a release PR with the appropriate version bump and changelog

#### Scenario: Release PR merged creates GitHub Release

- **WHEN** the release-please PR is merged
- **THEN** release-please SHALL create a GitHub Release with the new version tag

### Requirement: Release-please job outputs gate the build job

The release-please job SHALL expose `release_created` and `tag_name` as job outputs. The build-and-upload job SHALL run only when `release_created` is `true`.

#### Scenario: Release created triggers build

- **WHEN** release-please creates a new release (`release_created` is `true`)
- **THEN** the build-and-upload job SHALL execute with the `tag_name` output

#### Scenario: No release skips build

- **WHEN** release-please does not create a release (e.g., only updates a pending release PR)
- **THEN** the build-and-upload job SHALL NOT execute

### Requirement: Build produces binary with version and ldflags

The build job SHALL compile the binary using `go build` with `-ldflags="-s -w -X main.version=<tag_name>"` and `CGO_ENABLED=0`. The output binary SHALL be named `shadowdns-<goos>-<goarch>`.

#### Scenario: Binary built for linux/amd64

- **WHEN** the build matrix executes for `linux/amd64`
- **THEN** the job SHALL produce a statically-linked binary named `shadowdns-linux-amd64` with the release version embedded via ldflags

### Requirement: Build matrix supports future architecture expansion

The build job SHALL use `strategy.matrix.include` to define target platforms. The initial configuration SHALL include only `linux/amd64`, but the matrix structure SHALL allow adding new entries without modifying the workflow logic.

#### Scenario: Single architecture in matrix

- **WHEN** the workflow is initially deployed
- **THEN** the matrix SHALL contain exactly one entry: `goos: linux`, `goarch: amd64`

#### Scenario: Adding a new architecture

- **WHEN** a maintainer adds a new entry to the matrix `include` list (e.g., `linux/arm64`)
- **THEN** the build job SHALL produce binaries for all listed architectures without other workflow changes

### Requirement: Build produces deb package

The build job SHALL execute `make deb` to produce a `.deb` package after the binary is built. The nfpm tool SHALL be installed in the workflow before running `make deb`.

#### Scenario: Deb package produced

- **WHEN** the binary build succeeds
- **THEN** the job SHALL run `make deb` and produce a `.deb` file

### Requirement: Binary and deb uploaded to GitHub Release

The build job SHALL upload both the binary and the `.deb` package to the GitHub Release identified by `tag_name` using `gh release upload` with the `--clobber` flag.

#### Scenario: Assets uploaded to release

- **WHEN** both the binary and `.deb` package are produced
- **THEN** the job SHALL upload both files to the GitHub Release corresponding to the `tag_name`

### Requirement: Repo secret set via gh CLI

The `MY_RELEASE_PLEASE_TOKEN` secret SHALL be configured on the `chenwei791129/ShadowDNS` repository using `gh secret set`. The PAT value SHALL have minimal scope: `contents: write` and `pull-requests: write`.

#### Scenario: Secret created via CLI

- **WHEN** a maintainer runs `gh secret set MY_RELEASE_PLEASE_TOKEN --repo chenwei791129/ShadowDNS`
- **THEN** the secret SHALL be available to the release workflow

### Requirement: Release workflow permissions are minimal

The release workflow SHALL declare `permissions` with only `contents: write`, `issues: write`, and `pull-requests: write`. No additional permissions SHALL be granted.

#### Scenario: Workflow permissions are scoped

- **WHEN** the release workflow file is inspected
- **THEN** the `permissions` block SHALL contain exactly `contents: write`, `issues: write`, and `pull-requests: write`
