# Release Process

MLVM releases are built by GitHub Actions from a signed semantic-version tag. A release is
a reproducible source and binary checkpoint; it does not change the maturity labels in
[STATUS.md](STATUS.md).

## One-Time Setup

These need repository-owner access and are done once:

1. **Visibility.** Several pieces assume a public repository: `install.sh` is fetched from
   `raw.githubusercontent.com`, GitHub Pages on a free plan, the `ubuntu-24.04-arm` and
   `windows` runners in the determinism matrix, and build attestations (the release
   workflow skips attestation while the repository is private).
2. **GitHub Pages.** Settings → Pages → Source: *GitHub Actions*. The `Determinism and
   playground` workflow then publishes the playground, `determinism.json` and the
   `badge.json` behind the README badge on every push to `main`.
3. **Homebrew tap.** The release workflow writes `Formula/mlvm.rb` in the public
   repository `yao-jz/homebrew-tap` from the release checksums
   (`scripts/homebrew-formula.py`). It pushes with a deploy key that can write only to the
   tap (secret `HOMEBREW_TAP_DEPLOY_KEY`) or, alternatively, a fine-grained token with
   *Contents: read and write* on the tap (secret `HOMEBREW_TAP_TOKEN`). Without either, the
   step is skipped.

With an authenticated GitHub CLI, `./scripts/github-setup.sh` does steps 2 and 3 (Pages,
the tap repository, the deploy key and secret, and the formula for the latest release)
and then reports the latest workflow runs; `--status` only reports. Making the repository
public stays a manual decision.

## Before Tagging

1. Start from a clean worktree on the intended release commit.
2. Update `CHANGELOG.md`, including breaking CLI, Go API, state, witness, or contract changes.
3. Confirm `README.md`, `STATUS.md`, and `ROADMAP.md` describe the implementation at that
   commit.
4. Run the required gates:

```bash
make check
make test-all
make determinism
make build
bin/mlvm --version
```

5. Inspect the release configuration locally when GoReleaser is installed:

```bash
goreleaser release --snapshot --clean
```

The snapshot must produce `mlvm` archives for Linux, macOS, and Windows on `amd64` and
`arm64`. The regular CI workflow performs the same cross-platform build on every pull
request.

## Tag And Publish

Use a signed annotated tag. Pre-1.0 releases follow `v0.MINOR.PATCH`.

```bash
git tag -s v0.1.0 -m "MLVM v0.1.0"
git push origin v0.1.0
```

The `Release` workflow runs `make check`, invokes GoReleaser, attaches a GitHub build
attestation (public repositories), publishes archives and `checksums.txt` with the
`CHANGELOG.md` section for the version as release notes, and updates the Homebrew tap. Do not upload replacement binaries
to an existing tag. Create a patch release so source, checksums, and provenance stay aligned.

## Verify The Release

On both Linux and macOS, download the archive and checksum file from the GitHub release,
then verify the digest before running it:

```bash
archive=mlvm_0.1.0_darwin_arm64.tar.gz       # use the downloaded archive name
grep "  ${archive}$" checksums.txt
sha256sum "$archive"                        # Linux; compare with the line above
shasum -a 256 "$archive"                    # macOS; compare with the line above
./mlvm --version
```

With GitHub CLI installed, verify provenance against this repository:

```bash
gh attestation verify "$archive" --repo yao-jz/mlvm
```

Confirm that `--version` contains the release tag and source commit. Smoke-test `load-elf`,
`witness`, and a bounded `run` using a statically linked RV64IMAFD guest.

## Failed Releases

If the workflow fails before publishing, fix the cause and create a new tag. If any artifact
was published, leave the release visible with a failure note or mark it as withdrawn; never
move or reuse the tag. Security-sensitive failures follow [SECURITY.md](../.github/SECURITY.md).
