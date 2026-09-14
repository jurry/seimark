import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { writeFileSync, rmSync } from 'node:fs';

test('core tsconfig rejects a DOM reference', () => {
  const probe = new URL('../src/dom-probe.ts', import.meta.url);
  writeFileSync(probe, 'export const s: RTCRtpSender | null = null;\n');
  try {
    execFileSync('npx', ['tsc', '--noEmit', '-p', 'tsconfig.core.json'], {
      cwd: new URL('..', import.meta.url),
      encoding: 'utf8',
    });
    assert.fail('core compiled a DOM type; lib must exclude DOM');
  } catch (e) {
    const out = String((e as { stdout?: string }).stdout ?? '');
    assert.match(out, /RTCRtpSender/);
  } finally {
    rmSync(probe, { force: true });
  }
});
