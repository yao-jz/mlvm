#!/usr/bin/env bash
# github-setup.sh - the one-time, owner-side GitHub setup for MLVM releases, then a status
# report. Needs an authenticated GitHub CLI (`gh auth login`) with admin rights on the repo.
#
#   ./scripts/github-setup.sh            # set up, then report
#   ./scripts/github-setup.sh --status   # report only
#
# It
#   1. enables GitHub Pages with GitHub Actions as the source (the determinism workflow
#      deploys the playground and badge.json there; private repositories need a paid plan),
#   2. creates the public tap repository yao-jz/homebrew-tap if it does not exist,
#   3. gives the release workflow push access to the tap through a deploy key that can write
#      only to that repository (stored as the HOMEBREW_TAP_DEPLOY_KEY Actions secret),
#   4. writes Formula/mlvm.rb for the latest release that has a checksums.txt, and
#   5. prints the latest determinism, CI and release runs and the published releases.
# Each step is idempotent and skips work that is already done.
set -euo pipefail

REPO="${MLVM_REPO:-yao-jz/mlvm}"
TAP="${MLVM_TAP:-yao-jz/homebrew-tap}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

gh auth status >/dev/null 2>&1 || { echo "error: run 'gh auth login' first" >&2; exit 1; }

report() {
    echo "==> repository"
    gh repo view "$REPO" --json visibility,url --jq '"  \(.url) (\(.visibility | ascii_downcase))"'
    echo "==> latest workflow runs"
    for wf in determinism.yml ci.yml release.yml; do
        gh run list --repo "$REPO" --workflow "$wf" --limit 3 \
            --json displayTitle,headBranch,status,conclusion,url \
            --jq ".[] | \"  $wf  \(.headBranch)  \(.status)/\(.conclusion // \"-\")  \(.url)\"" 2>/dev/null \
            || echo "  $wf: no runs"
    done
    echo "==> releases"
    gh release list --repo "$REPO" --limit 5 || true
    echo "==> pages"
    gh api "repos/$REPO/pages" --jq '"  \(.html_url) (build: \(.build_type))"' 2>/dev/null || echo "  not enabled"
}

if [[ "${1:-}" == "--status" ]]; then
    report
    exit 0
fi

echo "==> GitHub Pages (source: GitHub Actions)"
if gh api "repos/$REPO/pages" >/dev/null 2>&1; then
    gh api -X PUT "repos/$REPO/pages" -f build_type=workflow >/dev/null && echo "  already enabled; source set to Actions"
elif gh api -X POST "repos/$REPO/pages" -f build_type=workflow >/dev/null 2>&1; then
    echo "  enabled"
else
    echo "  could not enable Pages (a private repository needs GitHub Pro, Team or Enterprise)" >&2
fi

echo "==> tap repository $TAP"
if gh repo view "$TAP" >/dev/null 2>&1; then
    echo "  exists"
else
    gh repo create "$TAP" --public --add-readme --description "Homebrew formulae for MLVM (github.com/$REPO)" >/dev/null
    echo "  created"
fi

echo "==> release workflow push access to $TAP"
if gh secret list --repo "$REPO" --json name --jq '.[].name' | grep -qx HOMEBREW_TAP_DEPLOY_KEY; then
    echo "  HOMEBREW_TAP_DEPLOY_KEY already set"
else
    keydir="$(mktemp -d)"
    trap 'rm -rf "$keydir"' EXIT
    ssh-keygen -q -t ed25519 -N "" -C "mlvm release -> homebrew-tap" -f "$keydir/key"
    gh repo deploy-key add "$keydir/key.pub" --repo "$TAP" --allow-write --title "mlvm release workflow" >/dev/null
    gh secret set HOMEBREW_TAP_DEPLOY_KEY --repo "$REPO" < "$keydir/key"
    rm -rf "$keydir"
    echo "  deploy key added to $TAP and stored as HOMEBREW_TAP_DEPLOY_KEY"
fi

echo "==> formula for the latest release"
tag="$(gh release list --repo "$REPO" --exclude-pre-releases --limit 1 --json tagName --jq '.[0].tagName' 2>/dev/null || true)"
if [[ -z "$tag" ]]; then
    echo "  no published release yet; the release workflow will write the formula"
else
    work="$(mktemp -d)"
    gh release download "$tag" --repo "$REPO" --pattern checksums.txt --dir "$work"
    python3 "$ROOT/scripts/homebrew-formula.py" --version "${tag#v}" --checksums "$work/checksums.txt" > "$work/mlvm.rb"
    gh repo clone "$TAP" "$work/tap" -- --depth 1 -q
    mkdir -p "$work/tap/Formula"
    cp "$work/mlvm.rb" "$work/tap/Formula/mlvm.rb"
    git -C "$work/tap" add Formula/mlvm.rb
    if git -C "$work/tap" diff --cached --quiet; then
        echo "  Formula/mlvm.rb is already at $tag"
    else
        git -C "$work/tap" commit -q -m "mlvm ${tag#v}"
        git -C "$work/tap" push -q
        echo "  Formula/mlvm.rb written for $tag"
    fi
    rm -rf "$work"
fi

report
