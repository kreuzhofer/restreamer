import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync, mkdirSync, existsSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {spawnSync} from 'node:child_process';

const repository = resolve(new URL('..', import.meta.url).pathname);
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'restreamer-install-'));
  t.after(() => rmSync(root, {recursive: true, force: true}));
  const directory = join(root, 'app');
  const env = {...process.env};
  const run = (...args) => spawnSync('bash', [join(repository, 'install.sh'),
    '--directory', directory, '--source-dir', repository, ...args], {encoding: 'utf8', env});
  return {root, directory, run, env};
}

test('prepare a private installation with unique credentials and automatic HTTPS', t => {
  const {directory, run} = fixture(t);
  const result = run('--domain', 'studio.example.com', '--prepare-only');
  assert.equal(result.status, 0, result.stderr);
  const env = readFileSync(join(directory, '.env'), 'utf8');
  const password = env.match(/^DASHBOARD_PASSWORD="([a-f0-9]{48})"$/m)?.[1];
  assert.ok(password, '192-bit generated password');
  assert.match(env, /^DASHBOARD_USERNAME="admin"$/m);
  assert.match(env, /^RESTREAMER_DOMAIN=studio.example.com$/m);
  assert.ok(/^INGESTION_SERVER_URL=rtmp:\/\/studio\.example\.com:1935\/live$/m.test(env), 'public RTMP address comes from installation domain');
  assert.ok(!/^BRB_ENABLED=/m.test(env), 'media authoring is always available, without an operator switch');
  assert.equal(statSync(join(directory, '.env')).mode & 0o777, 0o600);
  assert.equal(statSync(directory).mode & 0o777, 0o700);
  assert.match(readFileSync(join(directory, 'Caddyfile'), 'utf8'), /acme-v02\.api\.letsencrypt\.org/);
  assert.doesNotMatch(result.stdout + result.stderr, new RegExp(password));
  assert.match(result.stdout, /Prepared/);
  assert.doesNotMatch(result.stdout, /Ready at/);
});

test('launch waits for verified HTTPS and reruns preserve credentials and custom configuration', t => {
  const {root, directory, run, env} = fixture(t);
  const bin = join(root, 'bin');
  mkdirSync(bin);
  const adapter = join(bin, 'boundary');
  writeFileSync(adapter, `#!/usr/bin/env node
const fs = require('fs');
const name = require('path').basename(process.argv[1]);
const args = process.argv.slice(2);
fs.appendFileSync(process.env.CALL_LOG, JSON.stringify([name, ...args])+'\\n');
if (name === 'id') console.log('0');
if (name === 'uname') console.log('Linux');
if (name === 'stat') console.log('0');
if (name === 'docker' && args.includes('volume')) {
  if (process.env.FAIL_VOLUME_QUERY) process.exit(1);
  if (args.includes('ls')) {
    if (process.env.EXISTING_VOLUME) console.log('restreamer-standalone_restreamer-state');
    process.exit(0);
  }
  process.exit(process.env.EXISTING_VOLUME ? 0 : 1);
}
if (name === 'docker' && args.includes('up') && process.env.FAIL_UP) process.exit(1);
if (name === 'curl') {
  if (process.env.FAIL_TLS) process.exit(60);
  console.log(args.at(-1).endsWith('/api/dashboard') ? '401' : 'ok');
}
`);
  for (const name of ['id', 'uname', 'stat', 'docker', 'curl', 'systemctl', 'sleep', 'flock']) {
    writeFileSync(join(bin, name), readFileSync(adapter), {mode: 0o755});
  }
  env.PATH = `${bin}:${process.env.PATH}`;
  env.CALL_LOG = join(root, 'calls.jsonl');
  env.FAIL_VOLUME_QUERY = '1';
  let result = run('--domain', 'studio.example.com');
  assert.notEqual(result.status, 0, 'Docker query failures must not be treated as absent volumes');
  assert.doesNotMatch(result.stdout, /Ready at/);
  delete env.FAIL_VOLUME_QUERY;
  env.EXISTING_VOLUME = '1';
  result = run('--domain', 'studio.example.com');
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Saved standalone data already exists/);
  assert.doesNotMatch(result.stdout, /Ready at/);
  delete env.EXISTING_VOLUME;
  result = run('--domain', 'studio.example.com');
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Ready at https:\/\/studio.example.com/);
  const credentials = readFileSync(join(directory, '.env'), 'utf8').replace('INGESTION_SERVER_URL=rtmp://studio.example.com:1935/live', 'INGESTION_SERVER_URL=rtmp://ingest.example.com:1940/live');
  writeFileSync(join(directory, '.env'), credentials);
  const caddyfile = readFileSync(join(directory, 'Caddyfile'), 'utf8')+'\n# operator customization\n';
  writeFileSync(join(directory, 'Caddyfile'), caddyfile);
  result = run();
  assert.equal(result.status, 0, result.stderr);
  assert.equal(readFileSync(join(directory, '.env'), 'utf8'), credentials);
  env.FAIL_UP = '1';
  result = run();
  assert.notEqual(result.status, 0);
  assert.doesNotMatch(result.stdout, /Ready at/);
  assert.equal(readFileSync(join(directory, '.env'), 'utf8'), credentials);
  assert.equal(readFileSync(join(directory, 'Caddyfile'), 'utf8'), caddyfile);
  const calls = readFileSync(env.CALL_LOG, 'utf8').trim().split('\n').map(JSON.parse);
  const up = calls.filter(a => a[0] === 'docker' && a.includes('up'));
  assert.ok(up.length >= 1);
  assert.ok(up.every(a => a.includes('--no-recreate')), 'rerunning must not interrupt an existing broadcast');
  const checks = calls.filter(a => a[0] === 'curl');
  assert.ok(checks.some(a => a.includes('studio.example.com:443:127.0.0.1')));
  assert.ok(checks.every(a => !a.includes('-k') && !a.includes('--insecure')));
  delete env.FAIL_UP;
  env.FAIL_TLS = '1';
  result = run();
  assert.notEqual(result.status, 0);
  assert.doesNotMatch(result.stdout, /Ready at/);
  assert.match(result.stderr, /DNS|certificate|HTTPS/);
  assert.equal(readFileSync(join(directory, '.env'), 'utf8'), credentials);
});

test('failed downloads leave no partial installation or credentials', t => {
  const {root, directory, env} = fixture(t);
  const bin = join(root, 'bin');
  mkdirSync(bin);
  writeFileSync(join(bin, 'curl'), '#!/bin/sh\nexit 22\n', {mode: 0o755});
  env.PATH = `${bin}:${process.env.PATH}`;
  const result = spawnSync('bash', [join(repository, 'install.sh'), '--directory', directory,
    '--domain', 'studio.example.com', '--prepare-only'], {encoding: 'utf8', env});
  assert.notEqual(result.status, 0);
  assert.equal(existsSync(directory), false);
  assert.deepEqual(readdirSync(root), ['bin']);
  assert.doesNotMatch(result.stdout, /Ready at|Prepared/);
});

test('invalid domains and unmanaged directories are rejected without overwriting files', t => {
  const {directory, run} = fixture(t);
  for (const domain of ['https://studio.example.com', 'example.com {', 'example.com\nadmin off', '127.0.0.1', 'studio.local', 'a'.repeat(64)+'.com']) {
    const result = run('--domain', domain, '--prepare-only');
    assert.notEqual(result.status, 0, domain);
    assert.equal(existsSync(directory), false);
  }
  mkdirSync(directory);
  writeFileSync(join(directory, '.env'), 'existing credentials');
  const result = run('--domain', 'studio.example.com', '--prepare-only');
  assert.notEqual(result.status, 0);
  assert.equal(readFileSync(join(directory, '.env'), 'utf8'), 'existing credentials');
});

test('HTTPS smoke refuses existing Docker projects and query failures before cleanup can own resources', t => {
  const {root, env} = fixture(t);
  const bin = join(root, 'smoke-bin');
  mkdirSync(bin);
  const log = join(root, 'smoke-calls.jsonl');
  writeFileSync(join(bin, 'sudo'), `#!/usr/bin/env node
const fs = require('fs');
const args = process.argv.slice(2);
fs.appendFileSync(process.env.SMOKE_LOG, JSON.stringify(args)+'\\n');
if (args[0] === 'docker' && (args.includes('ps') || args.includes('volume'))) {
  if (process.env.SMOKE_CASE === 'query-failure') process.exit(1);
  if ((process.env.SMOKE_CASE === 'container' && args.includes('ps')) ||
      (process.env.SMOKE_CASE === 'volume' && args.includes('volume'))) console.log('existing-resource');
  if (process.env.SMOKE_CASE === 'unlabeled-volume' && args.includes('volume') && !args.includes('--filter')) console.log('restreamer-standalone_restreamer-state');
  process.exit(0);
}
if (args[0] === 'test') process.exit(0);
process.exit(99);
`, {mode: 0o755});
  for (const scenario of ['container', 'volume', 'unlabeled-volume', 'query-failure']) {
    writeFileSync(log, '');
    const result = spawnSync('bash', [join(repository, 'tests/install-smoke.sh')], {
      encoding: 'utf8', timeout: 10000,
      env: {...env, PATH: `${bin}:${env.PATH}`, SMOKE_LOG: log, SMOKE_CASE: scenario},
    });
    assert.notEqual(result.status, 0);
    const calls = readFileSync(log, 'utf8').trim().split('\n').map(JSON.parse);
    assert.ok(calls.every(args => args[0] === 'docker' &&
      (args.includes('ps') || args.includes('volume')) && !args.includes('down')),
    `must stop before install/cleanup: ${JSON.stringify(calls)}`);
  }
});

test('explicit initial dashboard credentials are private and reruns cannot replace them', t => {
  const {directory, run} = fixture(t);
  const password = `installer $literal # spaces "double" and 'single'`;
  const args = ['--domain', 'studio.example.com', '--username', 'studio-admin', '--password', password, '--prepare-only'];
  let result = run(...args);
  assert.equal(result.status, 0, result.stderr);
  const saved = readFileSync(join(directory, '.env'), 'utf8');
  assert.ok(saved.includes('DASHBOARD_USERNAME="studio-admin"'));
  assert.ok(!result.stdout.includes(password) && !result.stderr.includes(password), 'never print supplied password');
  result = run(...args); assert.equal(result.status, 0, result.stderr);
  assert.ok(readFileSync(join(directory, '.env'), 'utf8') === saved, 'identical rerun preserves saved credentials');
  result = run('--domain', 'studio.example.com', '--password', 'different-password', '--prepare-only');
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Credentials differ/);
  assert.ok(readFileSync(join(directory, '.env'), 'utf8') === saved, 'changed flags never rotate credentials');
});

test('initial credential validation rejects empty values and line injection before writing files', t => {
  for (const args of [['--username', ''], ['--username', 'name:other'], ['--password', ''], ['--password', 'line\nINJECTED=yes'], ['--password', 'line\rnext']]) {
    const {directory, run} = fixture(t);
    const result = run('--domain', 'studio.example.com', '--prepare-only', ...args);
    assert.notEqual(result.status, 0);
    assert.ok(!existsSync(directory));
  }
});

test('provided passwords survive real Compose interpolation', {skip: spawnSync('docker', ['compose', 'version']).status !== 0}, t => {
  const passwords = [`literal $value # spaces "double" and 'single'`, 'slash\\inside', 'trailing\\', "slash\\'quote", '\\n\\t', '$$double${HOME}'];
  for (const password of passwords) {
    const {directory, run} = fixture(t);
    const result = run('--domain', 'studio.example.com', '--username', 'studio-admin', '--password', password, '--prepare-only');
    assert.equal(result.status, 0, 'prepare succeeds');
    const compose = spawnSync('docker', ['compose', '--project-directory', directory, '--env-file', join(directory, '.env'), '-f', join(directory, 'docker-compose.yml'), 'config', '--environment'], {encoding: 'utf8'});
    assert.equal(compose.status, 0, 'Compose accepts the saved dotenv file');
    const values = Object.fromEntries(compose.stdout.split('\n').filter(line => line.includes('=')).map(line => [line.slice(0, line.indexOf('=')), line.slice(line.indexOf('=') + 1)]));
    assert.ok(values.DASHBOARD_PASSWORD === password, 'password reaches Compose unchanged');
  }
});
