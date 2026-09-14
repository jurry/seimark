import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { parseSEI } from '../src/sei.ts';
import { decodeMarker, isFormatUUID } from '../src/marker.ts';

const dir = fileURLToPath(new URL('../../vectors/nal/', import.meta.url));

for (const file of readdirSync(dir).filter((f) => f.endsWith('.bin'))) {
  const name = file.replace(/\.bin$/, '');
  test(`nal vector ${name}`, () => {
    const nal = new Uint8Array(readFileSync(dir + file));
    const want = JSON.parse(readFileSync(dir + name + '.json', 'utf8')) as Array<{
      sequence: number;
      origin_us: number;
      stream_id: string;
    }>;

    const got = parseSEI(nal)
      .filter((m) => m.uuid !== null && isFormatUUID(m.uuid))
      .map((m) => decodeMarker(m.body));

    assert.equal(got.length, want.length);
    for (const [i, m] of got.entries()) {
      assert.equal(m.sequence, want[i]!.sequence);
      assert.equal(m.originTimeUs, BigInt(want[i]!.origin_us));
      assert.equal(Buffer.from(m.streamId).toString('hex'), want[i]!.stream_id);
    }
  });
}
