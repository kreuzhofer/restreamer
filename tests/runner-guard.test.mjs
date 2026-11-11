import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync, writeFileSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';

const guard = fileURLToPath(new URL('../tools/ci/runner-guard.sh', import.meta.url));
function runGuard(overrides = {}, payloadOverrides = {}) {
  const directory = mkdtempSync(join(tmpdir(), 'restreamer-runner-guard-'));
  const eventPath = join(directory, 'event.json');
  writeFileSync(eventPath, JSON.stringify({repository: {full_name: 'kreuzhofer/restreamer', fork: false}, ref: 'refs/heads/main', ...payloadOverrides}));
  try {
    return spawnSync('bash', [guard], {encoding: 'utf8', env: {
      PATH: process.env.PATH,
      GITHUB_ACTIONS: 'true',
      GITHUB_REPOSITORY: 'kreuzhofer/restreamer',
      GITHUB_REF: 'refs/heads/main',
      GITHUB_EVENT_NAME: 'push',
      GITHUB_WORKFLOW_REF: 'kreuzhofer/restreamer/.github/workflows/ci.yml@refs/heads/main',
      GITHUB_EVENT_PATH: eventPath,
      ...overrides,
    }});
  } finally { rmSync(directory, {recursive: true, force: true}); }
}

test('context check passes for trusted main CI pushes and manual runs', () => {
  for (const event of ['push', 'workflow_dispatch']) {
    const result = runGuard({GITHUB_EVENT_NAME: event}, {ref: event === 'push' ? 'refs/heads/main' : 'main'});
    assert.equal(result.status, 0, result.stderr);
  }
});

test('context check reports failure for fork and PR contexts', () => {
  const cases = [
    [{GITHUB_EVENT_NAME: 'pull_request', GITHUB_REF: 'refs/pull/1/merge'}, {}],
    [{GITHUB_EVENT_NAME: 'pull_request_target'}, {}],
    [{}, {pull_request: {head: {repo: {fork: true}}}}],
    [{}, {repository: {full_name: 'kreuzhofer/restreamer', fork: true}}],
    [{GITHUB_REPOSITORY: 'someone/restreamer'}, {}],
    [{}, {repository: {full_name: 'someone/restreamer', fork: false}}],
  ];
  for (const [env, payload] of cases) {
    const result = runGuard(env, payload);
    assert.equal(result.status, 1, JSON.stringify({env, payload, result}));
    assert.match(result.stderr, /Refusing untrusted runner job/);
  }
});

test('context check reports failure for other workflows, branches, or missing context', () => {
  for (const env of [
    {GITHUB_REF: 'refs/heads/feature'},
    {GITHUB_WORKFLOW_REF: 'kreuzhofer/restreamer/.github/workflows/other.yml@refs/heads/main'},
    {GITHUB_WORKFLOW_REF: ''},
    {GITHUB_EVENT_NAME: ''},
    {GITHUB_ACTIONS: ''},
    {GITHUB_EVENT_PATH: '/does-not-exist'},
  ]) assert.equal(runGuard(env).status, 1, JSON.stringify(env));
  assert.equal(runGuard({}, {ref: 'refs/heads/feature'}).status, 1);
  assert.equal(runGuard({}, {repository: null}).status, 1);
});
