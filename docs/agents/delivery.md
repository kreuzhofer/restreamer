# Delivery completion

For `kreuzhofer/restreamer`, GitHub Actions is the required release-build
verification environment. Local tests and a local commit are intermediate steps.
Implementation work includes pushing the delivery branch and following its CI
run to completion, unless the user explicitly requests local-only work.

## Procedure

1. Run the applicable local checks and review the change. Consult
   `.github/workflows/ci.yml` for the current CI jobs and release conditions.
2. Commit and push to the branch selected for the task. Preserve any requested
   PR workflow; this policy does not authorize bypassing branch protection or
   merging a PR that is awaiting approval. Pushes to `main` publish release images.
3. Capture the full final commit SHA with `git rev-parse HEAD`. Find the `CI`
   run for that exact SHA, rather than relying on the latest run for a branch:

   ```sh
   delivery_sha=$(git rev-parse HEAD)
   gh run list --workflow CI --commit "$delivery_sha" \
     --json databaseId,headSha,status,conclusion,url
   ```

   If no run is visible yet, check again. Select a run whose `headSha` matches
   the pushed commit and watch its returned database ID:

   ```sh
   gh run watch <run-id> --exit-status --interval 30
   gh run view <run-id> --json headSha,status,conclusion,jobs,url
   ```

4. Wait until the entire workflow has `status: completed` and
   `conclusion: success`. Inspect the jobs as well: required tests, builds, and
   smoke checks must succeed. For a release on `main`, image building and
   publication must also succeed. A green test job while image publication is
   still running does not satisfy completion.
5. On failure, read the failed job logs, fix the cause, run relevant local
   checks, commit, push, and verify the new SHA. A cancelled, missing, skipped,
   or still-running required job is not successful verification. If an external
   blocker prevents completion, report the blocker and unfinished delivery
   explicitly; keep following a live run rather than ending merely because it
   takes time.
6. Reconcile the task's implementation tickets only after their acceptance
   criteria and the CI gate pass. Follow the project's issue-tracker workflow;
   resolve a parent only when its complete scope is delivered. A successful
   branch/PR run verifies that branch, not a release on `main`; verify the actual
   post-merge SHA before reporting a release.
7. Report the delivered commit SHA, successful CI run URL, and whether release
   images were published. Any subsequent commit requires its own CI evidence.
