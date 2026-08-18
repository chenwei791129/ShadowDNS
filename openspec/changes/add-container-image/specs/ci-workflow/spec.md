## ADDED Requirements

### Requirement: Pull Request CI validates the container image build

The CI workflow SHALL build the project Dockerfile for `linux/amd64` on every pull request targeting `main`. The build SHALL use the development version input and SHALL fail CI if the image cannot be built. This validation SHALL NOT log in to any container registry, reference a package-write credential, push an image, or run an image-level vulnerability scan.

#### Scenario: Pull Request image build succeeds

- **WHEN** a pull request targeting `main` contains a valid Dockerfile and build context
- **THEN** CI SHALL successfully build a local `linux/amd64` image with the development version input and SHALL NOT push it

#### Scenario: Pull Request image build fails

- **WHEN** the Dockerfile or build context cannot produce the `linux/amd64` image
- **THEN** the container build validation SHALL fail the CI workflow

#### Scenario: Fork Pull Request has no registry access

- **WHEN** an external contributor opens a pull request from a fork
- **THEN** the image build SHALL run using only repository read access and SHALL perform no registry login or package write

### Requirement: Pull Request CI verifies the image runtime contract

After building the local image, CI SHALL invoke a repository-owned verification script shared with local development. The script SHALL verify the image contract: architecture `amd64`, UID/GID `65532`, the ShadowDNS exec-form entrypoint, the complete default command including `0.0.0.0:5353`, exposed `5353/udp`, `5353/tcp`, and `9153/tcp` ports, absence of a Docker health check, and development version output. The checks SHALL fail when any inspected value differs from the container-image specification.

#### Scenario: Image contract matches

- **WHEN** the Pull Request image build completes
- **THEN** CI SHALL inspect the image and confirm every required runtime configuration value and SHALL run `--version` to confirm output `dev`

#### Scenario: Image contract drifts

- **WHEN** the built image has a root user, altered default command, missing port, configured health check, wrong architecture, wrong entrypoint, or non-dev default version
- **THEN** the runtime contract verification SHALL fail CI
