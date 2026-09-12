import { SeimarkError } from './errors.ts';
import { addEmulationPrevention, removeEmulationPrevention } from './emulation.ts';
import { FORMAT_UUID, UUID_SIZE } from './marker.ts';

const NAL_SEI = 0x06;
const PAYLOAD_TYPE_USER_DATA_UNREGISTERED = 5;
const RBSP_TRAILING = 0x80;

export interface SEIMessage {
  type: number;
  uuid: Uint8Array | null;
  body: Uint8Array;
}

function codeValue(n: number): number[] {
  const out: number[] = [];
  while (n >= 255) {
    out.push(0xff);
    n -= 255;
  }
  out.push(n);

  return out;
}

export function buildMarkerNAL(body: Uint8Array): Uint8Array {
  const size = UUID_SIZE + body.length;
  const rbsp = Uint8Array.from([
    PAYLOAD_TYPE_USER_DATA_UNREGISTERED,
    ...codeValue(size),
    ...FORMAT_UUID,
    ...body,
    RBSP_TRAILING,
  ]);

  return Uint8Array.from([NAL_SEI, ...addEmulationPrevention(rbsp)]);
}

export function parseSEI(nal: Uint8Array): SEIMessage[] {
  if (nal.length < 3) {
    throw new SeimarkError('unparsable_sei', `sei nal unit is ${nal.length} bytes`);
  }

  const rbsp = removeEmulationPrevention(nal.subarray(1));
  const messages: SEIMessage[] = [];
  let i = 0;

  while (i < rbsp.length && rbsp[i] !== RBSP_TRAILING) {
    let type = 0;
    while (rbsp[i] === 0xff) {
      type += 255;
      i++;
      if (i >= rbsp.length) throw new SeimarkError('unparsable_sei', 'payload type runs past the unit');
    }
    type += rbsp[i++] ?? 0;

    let size = 0;
    while (rbsp[i] === 0xff) {
      size += 255;
      i++;
      if (i >= rbsp.length) throw new SeimarkError('unparsable_sei', 'payload size runs past the unit');
    }
    if (i >= rbsp.length) throw new SeimarkError('unparsable_sei', 'payload size is missing');
    size += rbsp[i++] ?? 0;

    if (i + size > rbsp.length) {
      throw new SeimarkError('unparsable_sei', `payload size ${size} overruns the unit`);
    }

    const payload = rbsp.subarray(i, i + size);
    i += size;

    const unregistered = type === PAYLOAD_TYPE_USER_DATA_UNREGISTERED && size >= UUID_SIZE;
    messages.push({
      type,
      uuid: unregistered ? payload.subarray(0, UUID_SIZE) : null,
      body: unregistered ? payload.subarray(UUID_SIZE) : payload,
    });
  }

  return messages;
}
