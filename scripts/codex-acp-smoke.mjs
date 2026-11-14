#!/usr/bin/env node
/**
 * Smoke test for the vendored codex-acp single-file bundle.
 *
 * Spawns the bundle with CODEX_PATH pointing at the installed Codex CLI,
 * performs an ACP `initialize` handshake, and exits. It does NOT create a
 * session or send a prompt, so it never calls a model.
 *
 * Usage:
 *   CODEX_PATH=/path/to/codex node scripts/codex-acp-smoke.mjs [path/to/codex-acp.bundle.mjs]
 */
import { spawn, spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const defaultBundle = join(here, '..', 'resources', 'agents', 'codex-acp', 'codex-acp.bundle.mjs');
const bundle = resolve(process.argv[2] || defaultBundle);

if (!existsSync(bundle)) {
  console.error(`bundle not found: ${bundle}\nrun: scripts/vendor-codex-acp.sh`);
  process.exit(1);
}

const codexPath = process.env.CODEX_PATH || 'codex';
const which = spawnSync(codexPath, ['--version'], { encoding: 'utf8' });
if (which.error || which.status !== 0) {
  console.error(
    `Codex CLI not runnable: ${codexPath}\n` +
      'install Codex CLI and put it on PATH, or set CODEX_PATH to its absolute path',
  );
  process.exit(1);
}

const expectedVersion = (() => {
  try {
    return readFileSync(join(dirname(bundle), 'VERSION'), 'utf8').trim();
  } catch {
    return null;
  }
})();

const tmp = mkdtempSync(join(tmpdir(), 'codex-acp-smoke-'));
const child = spawn(process.execPath, [bundle], {
  cwd: tmp,
  env: { ...process.env, CODEX_PATH: codexPath },
  stdio: ['pipe', 'pipe', 'pipe'],
});

let buffer = '';
let stderr = '';
let timer;

function fail(message) {
  clearTimeout(timer);
  console.error(`FAIL: ${message}`);
  if (stderr.trim()) console.error(`stderr:\n${stderr.trim()}`);
  child.kill('SIGKILL');
  rmSync(tmp, { recursive: true, force: true });
  process.exit(1);
}

function succeed(result) {
  clearTimeout(timer);
  console.log(`OK: codex-acp ${result.agentInfo.name} -> ${JSON.stringify(result.agentInfo)}`);
  console.log(`    protocolVersion=${result.protocolVersion} authMethods=${result.authMethods?.length ?? 0}`);
  console.log(`    codex=${codexPath} (${which.stdout.trim()})`);
  console.log(`    bundle=${bundle}`);
  child.kill('SIGTERM');
  rmSync(tmp, { recursive: true, force: true });
  process.exit(0);
}

timer = setTimeout(() => fail('timed out waiting for initialize response'), 20_000);

child.stderr.setEncoding('utf8');
child.stderr.on('data', (chunk) => {
  stderr += chunk;
});

child.on('error', (error) => fail(`spawn error: ${error.message}`));
child.on('exit', (code, signal) => {
  if (code !== 0 && signal !== 'SIGTERM') fail(`agent exited early (code=${code}, signal=${signal})`);
});

child.stdout.setEncoding('utf8');
child.stdout.on('data', (chunk) => {
  buffer += chunk;
  let nl;
  while ((nl = buffer.indexOf('\n')) !== -1) {
    const line = buffer.slice(0, nl).trim();
    buffer = buffer.slice(nl + 1);
    if (!line) continue;

    let message;
    try {
      message = JSON.parse(line);
    } catch {
      fail(`non-JSON output on stdout: ${line.slice(0, 200)}`);
    }

    // Unknown agent -> client request; respond so the agent does not hang.
    if (message.method && message.id !== undefined) {
      child.stdin.write(
        JSON.stringify({
          jsonrpc: '2.0',
          id: message.id,
          error: { code: -32601, message: 'method not supported' },
        }) + '\n',
      );
      continue;
    }

    if (message.id !== 1) continue;
    if (message.error) fail(`initialize returned error: ${JSON.stringify(message.error)}`);

    const result = message.result ?? {};
    if (result.protocolVersion !== 1) fail(`unexpected protocolVersion: ${result.protocolVersion}`);
    if (result.agentInfo?.name !== '@agentclientprotocol/codex-acp') {
      fail(`unexpected agentInfo.name: ${result.agentInfo?.name}`);
    }
    if (expectedVersion && result.agentInfo.version !== expectedVersion) {
      fail(`agentInfo.version ${result.agentInfo.version} != VERSION ${expectedVersion}`);
    }
    if (!Array.isArray(result.authMethods) || result.authMethods.length === 0) {
      fail('initialize did not return authMethods');
    }
    succeed(result);
  }
});

child.stdin.write(
  JSON.stringify({
    jsonrpc: '2.0',
    id: 1,
    method: 'initialize',
    params: {
      protocolVersion: 1,
      clientCapabilities: {
        fs: { readTextFile: false, writeTextFile: false },
        terminal: false,
      },
    },
  }) + '\n',
);
