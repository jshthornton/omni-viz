# omniviz in CI/CD

omniviz was designed CI-first: `omniviz test` exits non-zero on regressions,
writes `report.json` + `junit.xml` (GitLab test reports, GitHub test-reporter
actions) and `--fail-on-new` treats unapproved shots as failures. This page
covers GitHub Actions, GitLab CI and the baseline-management strategies that
make PR flows work.

## The branching story

Baselines live in **git** (`baseline_dir/` is committed). That gives you the
branch/baseline semantics the hosted tools build servers for, for free:

- A PR diffs its captures against **the baselines on the base branch** (git
  merge semantics) — exactly the Chromatic/Percy "compare against baseline
  build" flow.
- Approved changes are baseline updates **in the same PR** — reviewable,
  revertable, no server state to drift.
- No artifact storage, no network, no service account. CI only needs the
  checkout.

Trade-off: baselines are per-repo, not per-environment. If CI renders with
different fonts/GPU than the machine that took the baselines, take the
baselines *from CI* (see "Seeding and accepting baselines" below).

## GitHub Actions

### Ready-made composite action

```yaml
name: visual
on: [pull_request, push]
permissions:
  contents: read
  pull-requests: write   # PR comments

jobs:
  visual:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: jshthornton/omni-viz/.github/actions/omniviz@main
        with:
          project: .
          fail-on-new: ${{ github.event_name == 'pull_request' }}
```

The action installs the CLI from the action ref, runs `omniviz test`,
appends the markdown report to the **job summary** and comments on the PR,
uploads `report.json`, `junit.xml`, `current/`, `diff/` and logs as
artifacts, then exits with the test result.

### Hand-rolled workflow

```yaml
name: visual
on: [pull_request, push]
permissions:
  contents: read
  pull-requests: write

jobs:
  visual:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }        # baselines history

      - uses: actions/setup-go@v5
        with: { go-version: stable }

      - run: go install github.com/jshthornton/omni-viz/cmd/omniviz@main

      # your targets:
      # - web: needs a Chrome-family browser (ubuntu-latest has chromium;
      #   set [driver.web] browser = "chromium" or OMNIVIZ_BROWSER)
      # - godot: needs a GPU runner or a self-hosted box (software GL is slow
      #   but works for stills)
      # - android: an emulator service container (see docker/android)

      - run: omniviz test --fail-on-new

      - run: omniviz summary --markdown >> "$GITHUB_STEP_SUMMARY"

      - name: Comment on PR
        if: github.event_name == 'pull_request'
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          omniviz summary --markdown > comment.md
          gh pr comment "$NUM" --replace --body-file comment.md
        # NUM: use github.event.pull_request.number via env mapping

      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: omniviz-report
          path: |
            tmp/omniviz/report.json
            tmp/omniviz/junit.xml
            tmp/omniviz/current/
            tmp/omniviz/diff/
          if-no-files-found: ignore

      # native test reporting (GitHub Apps like Test Reporter read junit.xml)
      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: junit
          path: tmp/omniviz/junit.xml
```

### Seeding and accepting baselines

First run (and after intentional visual changes) shots come back **new**.
Three ways to accept them:

1. **Locally** (best): run `omniviz capture && omniviz approve --all` (or
   cherry-pick keys), review the images, commit `baseline_dir/`.
2. **From CI artifacts**: download the `omniviz-report` artifact, drop
   `current/*.png` into `baseline_dir/` locally, review the diff, commit.
3. **Auto-accept on the default branch** — for projects that treat main as
   the source of truth:

```yaml
jobs:
  accept:
    if: github.ref == 'refs/heads/main' && github.event_name == 'push'
    runs-on: ubuntu-latest
    permissions: { contents: write }
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: stable }
      - run: go install github.com/jshthornton/omni-viz/cmd/omniviz@main
      - run: omniviz test || true                 # capture + diff
      - run: |
          omniviz approve --all                   # adopt new shots
          git config user.name omniviz-bot
          git config user.email bot@users.noreply.github.com
          git add baseline_dir
          git diff --cached --quiet || git commit -m "omniviz: update baselines [skip ci]"
          git push
```

Use auto-accept only on protected branches you trust; PRs should stay on
`--fail-on-new` so new shots get a human look before merge.

### Rendering differences between developer machines and CI

Fonts, GPU dithering and anti-aliasing differ per environment. Two rules:

- Take baselines **in the same environment CI uses** (the container-verified
  flows in `docker/` exist for exactly this: `docker/winforms`, `docker/delphi`,
  `docker/android` pin the renderer).
- Or absorb per-environment noise with config: `max_diff_ratio` tolerates a
  small fraction of AA pixels, `ignore_regions` excludes dynamic strips
  (clocks, ads, notification badges), `max_changed` caps global drift.

## GitLab CI

```yaml
visual:
  stage: test
  image: golang:1.27
  variables:
    GIT_STRATEGY: clone
  script:
    - go install github.com/jshthornton/omni-viz/cmd/omniviz@main
    # browser targets: apt-get install -y chromium and point
    # [driver.web] browser at it
    - omniviz test --fail-on-new
    - omniviz summary --markdown          # pipe into MR description via API if desired
  artifacts:
    when: always
    paths:
      - tmp/omniviz/report.json
      - tmp/omniviz/current/
      - tmp/omniviz/diff/
    reports:
      junit: tmp/omniviz/junit.xml        # shows up in MRs → Tests tab
```

MR pipelines diff against the target branch's baselines automatically (git
checkout semantics). Accept baselines on the default branch with the same
approve-and-push pattern as above (`omniviz test || true; omniviz approve
--all; git commit`).

## Exit codes and statuses

| exit | meaning |
|---|---|
| 0 | all shots pass (or `capture` finished) |
| 1 | shots need attention: `fail`, `size`, `error` or `missing`; with `--fail-on-new` also unapproved `new` |
| 2 | usage error |

Statuses: `pass`, `fail` (per-pixel threshold or changed-area budget), `new`
(no baseline), `size` (resolution changed), `error` (capture failed),
`missing` (reserved). `captured` is a non-compared capture (`omniviz
capture`).

## Release binaries

Tags (`git tag v0.2.0 && git push --tags`) run GoReleaser (`.goreleaser.yaml`)
and attach linux/darwin/windows binaries to the release. CI on machines
without Go can install with:

```bash
curl -fsSL https://github.com/jshthornton/omni-viz/releases/latest/download/omniviz_VERSION_OS_ARCH.tar.gz | tar xz
```

Until the first release, `go install ...@main` (or a pinned SHA) works
everywhere Go does.
