import type { FullConfig, FullResult, Reporter, Suite, TestCase, TestResult } from '@playwright/test/reporter';
import { createHash } from 'node:crypto';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { SECURITY_FIXTURE_CONFIGURATION } from './configuration.ts';

const required = ['stored-content', 'hostile-origin', 'oracle-negative-controls'];
type Observation = { id: string; status: string; duration_ms: number };

export function completeSecurityProfile(status: string, error: boolean, tests: readonly Observation[]): boolean {
  return !error && status === 'passed' && tests.length === required.length &&
    required.every((id) => tests.filter((test) => test.id === id && test.status === 'passed').length === 1);
}

/** A filtered or skipped run cannot produce a passing security receipt. */
export default class SecurityReceiptReporter implements Reporter {
  private tests: Observation[] = [];
  private started = Date.now();
  private error = false;
  private binaryDigest: string | null = null;

  onBegin(_config: FullConfig, _suite: Suite): void {
    const binary = process.env['HIKYO_E2E_BINARY'];
    if (binary !== undefined) {
      this.binaryDigest = createHash('sha256').update(readFileSync(binary)).digest('hex');
    }
  }

  onTestEnd(test: TestCase, result: TestResult): void {
    const id = test.title.split(':')[0] ?? '';
    this.tests.push({ id, status: result.status, duration_ms: result.duration });
  }

  onError(): void { this.error = true; }

  async onEnd(result: FullResult): Promise<{ status: 'passed' | 'failed' }> {
    const passed = completeSecurityProfile(result.status, this.error, this.tests);
    const receipt = {
      schema_version: 1, profile: 'hikyo-browser-security-v1',
      status: passed ? 'passed' : 'failed',
      artifact_kind: this.binaryDigest === null ? 'development-build' : 'provided-binary',
      binary_sha256: this.binaryDigest,
      source_sha: process.env['HIKYO_SECURITY_SOURCE_SHA'] ?? null,
      datastore: 'sqlite', authentication: 'native-password-totp',
      fixture_configuration: SECURITY_FIXTURE_CONFIGURATION,
      elapsed_ms: Date.now() - this.started,
      required_tests: required, tests: this.tests,
      limitations: ['Chromium desktop only', 'Native-auth markup/CORS regression profile, not full production server configuration',
        'Loopback HTTP --dev transport', 'Seed has a 300-second development disclosure window and zero-second protected window',
        'No external identity provider, remote-directory or deployment-adapter coverage'],
    };
    const path = resolve(process.env['HIKYO_SECURITY_BROWSER_RECEIPT'] ?? 'test-results/security/receipt.json');
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, JSON.stringify(receipt, null, 2) + '\n');
    return { status: passed ? 'passed' : 'failed' };
  }
}
