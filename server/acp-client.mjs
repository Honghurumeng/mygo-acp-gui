import { spawn } from 'node:child_process';
import { EventEmitter } from 'node:events';

/**
 * Minimal ACP client: speaks newline-delimited JSON-RPC 2.0 to an ACP agent
 * process over stdio.
 *
 * Emits:
 *   notification  ({ method, params })        agent -> client notifications
 *   permission    ({ id, params, respond })    session/request_permission
 *   stderr        (text)
 *   exit          ({ code, signal })
 *   fatal         (Error)
 *   protocolError ({ line, error })
 */
export class AcpClient extends EventEmitter {
  #child = null;
  #buffer = '';
  #nextId = 0;
  #pending = new Map();
  #exited = false;

  constructor({ command = 'npx', args = ['-y', '@geohar/pi-acp'], cwd, env = {} } = {}) {
    super();
    this.command = command;
    this.args = args;
    this.cwd = cwd || process.cwd();
    this.env = { ...process.env, ...env };
  }

  start() {
    this.#child = spawn(this.command, this.args, {
      cwd: this.cwd,
      env: this.env,
      stdio: ['pipe', 'pipe', 'pipe'],
      detached: true,
    });

    this.#child.on('error', (err) => this.emit('fatal', err));
    this.#child.on('exit', (code, signal) => {
      this.#exited = true;
      for (const [, entry] of this.#pending) {
        entry.reject(new Error(`agent exited (code=${code}, signal=${signal})`));
      }
      this.#pending.clear();
      this.emit('exit', { code, signal });
    });

    this.#child.stdout.setEncoding('utf8');
    this.#child.stdout.on('data', (chunk) => this.#ingest(chunk));

    this.#child.stderr.setEncoding('utf8');
    this.#child.stderr.on('data', (text) => this.emit('stderr', text));

    return this;
  }

  #ingest(chunk) {
    this.#buffer += chunk;
    let nl;
    while ((nl = this.#buffer.indexOf('\n')) !== -1) {
      const line = this.#buffer.slice(0, nl).trim();
      this.#buffer = this.#buffer.slice(nl + 1);
      if (!line) continue;
      let msg;
      try {
        msg = JSON.parse(line);
      } catch (error) {
        this.emit('protocolError', { line, error });
        continue;
      }
      this.#dispatch(msg);
    }
  }

  #dispatch(msg) {
    if (msg.method && msg.id !== undefined) {
      this.#onAgentRequest(msg);
      return;
    }
    if (msg.method) {
      this.emit('notification', msg);
      return;
    }
    const entry = this.#pending.get(msg.id);
    if (!entry) return;
    this.#pending.delete(msg.id);
    if (msg.error) {
      const err = new Error(msg.error.message || 'agent error');
      err.code = msg.error.code;
      err.data = msg.error.data;
      entry.reject(err);
    } else {
      entry.resolve(msg.result);
    }
  }

  #onAgentRequest(msg) {
    if (msg.method === 'session/request_permission') {
      let settled = false;
      const respond = (result) => {
        if (settled) return;
        settled = true;
        this.#write({ jsonrpc: '2.0', id: msg.id, result });
      };
      this.emit('permission', { id: msg.id, params: msg.params, respond });
      return;
    }
    // We advertise no fs/terminal capabilities, so anything else is unexpected.
    this.#write({
      jsonrpc: '2.0',
      id: msg.id,
      error: { code: -32601, message: `method not supported by this client: ${msg.method}` },
    });
  }

  #write(payload) {
    if (!this.#child || this.#exited || !this.#child.stdin.writable) return;
    this.#child.stdin.write(`${JSON.stringify(payload)}\n`);
  }

  request(method, params) {
    const id = ++this.#nextId;
    return new Promise((resolve, reject) => {
      if (!this.#child || this.#exited) {
        reject(new Error('agent process is not running'));
        return;
      }
      this.#pending.set(id, { resolve, reject, method });
      this.#write({ jsonrpc: '2.0', id, method, params });
    });
  }

  notify(method, params) {
    this.#write({ jsonrpc: '2.0', method, params });
  }

  async stop() {
    if (!this.#child || this.#exited) return;
    try {
      // Negative pid targets the whole process group (npx spawns a child).
      process.kill(-this.#child.pid, 'SIGTERM');
    } catch {
      try {
        this.#child.kill('SIGTERM');
      } catch {
        /* already gone */
      }
    }
    await new Promise((resolve) => setTimeout(resolve, 300));
    try {
      process.kill(-this.#child.pid, 'SIGKILL');
    } catch {
      /* already gone */
    }
  }
}
