import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { decodeMarker, encodeMarker } from '../src/marker.ts';
import { SeimarkError } from '../src/errors.ts';

const dir = fileURLToPath(new URL('../../vectors/markers/', import.meta.url));

interface VectorJSON {
  version?: number;
  time_source?: 'send' | 'capture';
  origin_us?: number;
  sequence?: number;
  stream_id?: string;
  payload?: string | null;
  error?: string;
}

const hex = (b: Uint8Array) => Buffer.from(b).toString('hex');

for (const file of readdirSync(dir).filter((f) => f.endsWith('.bin'))) {
  const name = file.replace(/\.bin$/, '');
  test(`vector ${name}`, () => {
    const body = new Uint8Array(readFileSync(dir + file));
    const want = JSON.parse(
      readFileSync(dir + name + '.json', 'utf8'),
    ) as VectorJSON;

    if (want.error) {
      let err: SeimarkError | undefined;
  try { decodeMarker(body); } catch (e) { err = e as SeimarkError; }
      assert.equal(err?.code, want.error);
      return;
    }

    const got = decodeMarker(body);
    assert.equal(got.timeSource, want.time_source);
    assert.equal(got.originTimeUs, BigInt(want.origin_us!));
    assert.equal(got.sequence, want.sequence);
    assert.equal(hex(got.streamId), want.stream_id);
    if (want.payload == null) {
      assert.equal(got.payload, null);
    } else {
      assert.equal(
        Buffer.from(got.payload!).toString('base64'),
        want.payload,
      );
    }
    assert.deepEqual(encodeMarker(got), body.subarray(0, encodeMarker(got).length));
  });
}
