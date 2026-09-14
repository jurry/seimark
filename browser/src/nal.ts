import { SeimarkError } from './errors.ts';

export type Framing = 'annexb' | 'length';

export interface NALUnit {
  data: Uint8Array;
  startCodeLength: 3 | 4;
}

const LENGTH_SIZE = 4;

export function nalType(unit: Uint8Array): number {
  return (unit[0] ?? 0) & 0x1f;
}

export function detectFraming(au: Uint8Array): Framing | null {
  if (au.length >= 4 && au[0] === 0 && au[1] === 0 && au[2] === 0 && au[3] === 1) {
    return 'annexb';
  }
  if (au.length >= 3 && au[0] === 0 && au[1] === 0 && au[2] === 1) {
    return 'annexb';
  }
  if (au.length > LENGTH_SIZE) {
    const n = new DataView(au.buffer, au.byteOffset, au.byteLength).getUint32(0);
    if (n >= 1 && n <= au.length - LENGTH_SIZE) {
      return 'length';
    }
  }

  return null;
}

function startCodeAt(au: Uint8Array, i: number): 3 | 4 | 0 {
  if (au[i] === 0 && au[i + 1] === 0) {
    if (au[i + 2] === 1) return 3;
    if (au[i + 2] === 0 && au[i + 3] === 1) return 4;
  }

  return 0;
}

function trimTrailingZeros(u: Uint8Array): Uint8Array {
  let end = u.length;
  while (end > 0 && u[end - 1] === 0) end--;

  return u.subarray(0, end);
}

export function nalUnits(au: Uint8Array, framing: Framing): NALUnit[] {
  return framing === 'annexb' ? annexBUnits(au) : lengthUnits(au);
}

function annexBUnits(au: Uint8Array): NALUnit[] {
  const units: NALUnit[] = [];
  let i = 0;

  while (i < au.length && startCodeAt(au, i) === 0) i++;

  while (i < au.length) {
    const scl = startCodeAt(au, i);
    if (scl === 0) break;
    const start = i + scl;
    let end = start;
    while (end < au.length && startCodeAt(au, end) === 0) end++;

    const data = trimTrailingZeros(au.subarray(start, end));
    if (data.length > 0) {
      units.push({ data, startCodeLength: scl });
    }
    i = end;
  }

  return units;
}

function lengthUnits(au: Uint8Array): NALUnit[] {
  const units: NALUnit[] = [];
  const view = new DataView(au.buffer, au.byteOffset, au.byteLength);
  let i = 0;

  while (i < au.length) {
    if (i + LENGTH_SIZE > au.length) {
      throw new SeimarkError('truncated', `nal unit length at byte ${i} is cut short`);
    }
    const n = view.getUint32(i);
    if (n < 1 || i + LENGTH_SIZE + n > au.length) {
      throw new SeimarkError('truncated', `nal unit length ${n} at byte ${i} overruns the access unit`);
    }
    units.push({
      data: au.subarray(i + LENGTH_SIZE, i + LENGTH_SIZE + n),
      startCodeLength: 4,
    });
    i += LENGTH_SIZE + n;
  }

  return units;
}

export function joinNALUnits(units: NALUnit[], framing: Framing): Uint8Array {
  const size = units.reduce(
    (n, u) => n + (framing === 'annexb' ? u.startCodeLength : LENGTH_SIZE) + u.data.length,
    0,
  );
  const out = new Uint8Array(size);
  const view = new DataView(out.buffer);
  let i = 0;

  for (const u of units) {
    if (framing === 'annexb') {
      if (u.startCodeLength === 4) out[i++] = 0;
      out[i++] = 0;
      out[i++] = 0;
      out[i++] = 1;
    } else {
      view.setUint32(i, u.data.length);
      i += LENGTH_SIZE;
    }
    out.set(u.data, i);
    i += u.data.length;
  }

  return out;
}
