import { spawn, spawnSync } from 'node:child_process';
import { appendFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { z } from 'zod';

const STATE = fileURLToPath(new URL('../.auth/mail.json', import.meta.url));
const REPOSITORY = fileURLToPath(new URL('../../..', import.meta.url));
const zState = z.object({ messages: z.string(), failure: z.string() });
const zMessage = z.object({ kind: z.literal('message'), from: z.string(), recipients: z.array(z.string()), data: z.string(), tls: z.enum(['TLSv1.2', 'TLSv1.3']) });
const zEvent = z.discriminatedUnion('kind', [zMessage, z.object({ kind: z.literal('ready'), addr: z.string(), ca_pem: z.string() })]);

/** Read only SMTP-accepted messages across Playwright's setup/worker boundary. */
export function readMail(recipient: string, stateFile = STATE) {
  const state = zState.parse(JSON.parse(readFileSync(stateFile, 'utf8')));
  if (existsSync(state.failure)) throw new Error(readFileSync(state.failure, 'utf8'));
  return readFileSync(state.messages, 'utf8').split('\n').slice(0, -1).filter(Boolean)
    .map((line) => zMessage.parse(JSON.parse(line)))
    .filter((message) => message.recipients.includes(recipient));
}

/** Run the existing Go TLS SMTP sink; product certificate and egress checks stay enabled. */
export async function startMailSink(stateFile = STATE) {
  const directory = mkdtempSync(join(tmpdir(), 'hikyo-browser-mail-'));
  const certificate = join(directory, 'cert.pem');
  const messages = join(directory, 'messages.jsonl');
  const failure = join(directory, 'failure.txt');
  const binary = process.env['HIKYO_E2E_MAIL_BINARY'] ?? join(directory, 'mail-sink');
  let stopped = false;
  let published = false;
  writeFileSync(messages, '', { mode: 0o600 });
  if (process.env['HIKYO_E2E_MAIL_BINARY'] === undefined) {
    const build = spawnSync('go', ['build', '-o', binary, './internal/mailtest/cmd'], { cwd: REPOSITORY, encoding: 'utf8' });
    if (build.status !== 0) {
      rmSync(directory, { recursive: true, force: true });
      throw new Error(`mail sink build failed: ${build.stderr}`);
    }
  }
  const processHandle = spawn(binary, [], { cwd: REPOSITORY, stdio: ['pipe', 'pipe', 'pipe'] });
  const closed = new Promise<void>((resolve) => { processHandle.once('close', () => resolve()); });
  let stderr = '';
  processHandle.stderr.setEncoding('utf8');
  processHandle.stdout.setEncoding('utf8');
  processHandle.stderr.on('data', (chunk: string) => { stderr = (stderr + chunk).slice(-8192); });
  const stop = async () => {
    stopped = true;
    if (published) rmSync(stateFile, { force: true });
    const kill = setTimeout(() => processHandle.kill('SIGKILL'), 1_000);
    try {
      processHandle.stdin.end();
      // close follows process exit and complete drainage of its stdout/stderr.
      await closed;
    } finally {
      clearTimeout(kill);
    }
    rmSync(directory, { recursive: true, force: true });
  };
  try {
    const ready = await new Promise<z.infer<typeof zEvent> & { kind: 'ready' }>((resolve, reject) => {
      let buffered = '';
      let announced = false;
      const deadline = setTimeout(() => reject(new Error(`mail sink did not start: ${stderr}`)), 10_000);
      processHandle.once('error', (error) => { clearTimeout(deadline); reject(error); });
      processHandle.once('exit', (code) => {
        clearTimeout(deadline);
        if (stopped) return;
        const message = `mail sink exited (${String(code)}): ${stderr}`;
        writeFileSync(failure, message);
        reject(new Error(message));
      });
      processHandle.stdout.on('data', (chunk: string) => {
        if (stopped) return;
        buffered += chunk;
        for (;;) {
          const boundary = buffered.indexOf('\n');
          if (boundary < 0) break;
          const line = buffered.slice(0, boundary);
          buffered = buffered.slice(boundary + 1);
          try {
            const event = zEvent.parse(JSON.parse(line));
            if (event.kind === 'ready' && !announced) {
              announced = true;
              clearTimeout(deadline);
              resolve(event);
            } else if (event.kind === 'message' && announced) {
              appendFileSync(messages, `${JSON.stringify(event)}\n`);
            } else {
              throw new Error('mail sink emitted an event out of order');
            }
          } catch (error) {
            clearTimeout(deadline);
            writeFileSync(failure, String(error));
            reject(error);
            processHandle.kill('SIGTERM');
          }
        }
      });
    });
    writeFileSync(certificate, ready.ca_pem, { mode: 0o600 });
    mkdirSync(dirname(stateFile), { recursive: true });
    writeFileSync(stateFile, JSON.stringify({ messages, failure }), { mode: 0o600 });
    published = true;
    return {
      environment: {
        HIKYO_MAIL_ADDR: ready.addr, HIKYO_MAIL_TLS: 'implicit',
        HIKYO_MAIL_FROM: 'Hikyo <signup@hikyo.test>', HIKYO_MAIL_ALLOWED_CIDRS: '127.0.0.1/32',
        HIKYO_MAIL_CA_FILE: certificate, HIKYO_MAIL_USER: '', HIKYO_MAIL_PASSWORD: '',
        HIKYO_MAIL_PASSWORD_FILE: '', HIKYO_MAIL_EHLO: 'browser.hikyo.test',
      }, stop,
    };
  } catch (error) {
    try {
      await stop();
    } catch (cleanupError) {
      throw new AggregateError([error, cleanupError], 'mail sink startup failed and cleanup also failed');
    }
    throw error;
  }
}
