import { type IncomingMessage, Agent, get } from 'node:http';
import { connect } from 'node:net';
import { expect, it } from 'vitest';

import { startCanary } from './canary.ts';

it('closes a completed keep-alive connection without waiting for its idle timeout', async () => {
  const canary = await startCanary();
  const agent = new Agent({ keepAlive: true });
  let closing: Promise<void> | undefined;
  try {
    const received = new Promise<IncomingMessage>((resolve) => canary.server.once('request', resolve));
    await new Promise<void>((resolve, reject) => {
      get(`${canary.origin}/idle`, { agent }, (response) => {
        response.on('error', reject);
        response.on('end', resolve);
        response.resume();
      }).on('error', reject);
    });
    const request = await received;
    expect(request.complete).toBe(true);
    expect(request.socket.destroyed).toBe(false);
    closing = canary.close();
    expect(canary.server.listening).toBe(false);
    expect(request.socket.destroyed).toBe(true);
    await closing;
    expect(canary.hits).toEqual(['/idle']);
  } finally {
    agent.destroy();
    canary.server.closeAllConnections();
    await (closing ?? canary.close());
  }
});

it('stops accepting and closes an unfinished request during teardown', async () => {
  const canary = await startCanary();
  const origin = new URL(canary.origin);
  const socket = connect(Number(origin.port), origin.hostname);
  let closing: Promise<void> | undefined;
  try {
    await new Promise<void>((resolve, reject) => {
      socket.once('error', reject);
      socket.once('connect', resolve);
    });
    const received = new Promise<IncomingMessage>((resolve) => canary.server.once('request', resolve));
    socket.write('POST /unfinished HTTP/1.1\r\nHost: canary\r\nContent-Length: 10\r\nConnection: keep-alive\r\n\r\nx');
    const request = await received;
    expect(request.complete).toBe(false);
    expect(request.socket.destroyed).toBe(false);
    closing = canary.close();
    expect(canary.server.listening).toBe(false);
    expect(request.socket.destroyed).toBe(true);
    await closing;
    expect(canary.hits).toEqual(['/unfinished']);
  } finally {
    socket.destroy();
    canary.server.closeAllConnections();
    await (closing ?? canary.close());
  }
});
