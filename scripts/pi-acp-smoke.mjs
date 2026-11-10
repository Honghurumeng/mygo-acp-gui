#!/usr/bin/env node
/**
 * Smoke test for the vendored pi-acp single-file bundle.
 *
 * Spawns the bundle, performs an ACP `initialize` handshake, verifies the
 * response, then exits. Does NOT create a session, so it never spawns `pi`
 * and never touches the user's credentials.
 *
 * Usage:
 *   node scripts/pi-acp-smoke.mjs [path/to/pi-acp.bundle.mjs]
 */
import { spawn } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const defaultBundle = join(here, '..', 'resources', 'agents', 'pi-acp', 'pi-acp.bundle.mjs');
const bundle = resolve(process.argv[2] || defaultBundle);

if (!existsSync(bundle)) {
  console.error(`bundle not found: ${bundle}\nrun: scripts/vendor-pi-acp.sh`);
  process.exit(1);
}

const expectedVersion = (() => {
  try {
    return readFileSync(join(dirname(bundle), 'VERSION'), 'utf8').trim();
  } catch {
    return null;
  }
})();

const tmp = mkdtempSync(join(tmpdir(), 'pi-acp-smoke-'));
const child = spawn(process.execPath, [bundle], {
  cwd: tmp,
  env: { ...process.env, PI_CODING_AGENT_DIR: join(tmp, 'agent') },
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
  console.log(`OK: pi-acp ${result.agentInfo.title} -> ${JSON.stringify(result.agentInfo)}`);
  console.log(`    protocolVersion=${result.protocolVersion} authMethods=${result.authMethods?.length ?? 0}`);
  console.log(`    bundle=${bundle}`);
  child.kill('SIGTERM');
  rmSync(tmp, { recursive: true, force: true });
  process.exit(0);
}

timer = setTimeout(() => fail('timed out waiting for initialize response'), 15_000);

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
    if (result.agentInfo?.name !== '@geohar/pi-acp') {
      fail(`unexpected agentInfo.name: ${result.agentInfo?.name} (is package.json next to the bundle?)`);
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
