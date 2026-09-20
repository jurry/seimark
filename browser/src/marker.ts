import { SeimarkError } from './errors.ts';

/** The marker format version this implementation writes and is the only one it reads. */
export const VERSION = 1;
export const FIXED_SIZE = 22;
/**
 * Payload size above which a mark still succeeds but reports a
 * `payload_above_soft_limit` warning, since a large payload is carried on every
 * marked frame.
 */
export const PAYLOAD_SOFT_LIMIT = 4096;
/** Payload size above which encoding is refused: the length field is 16 bits. */
export const PAYLOAD_HARD_LIMIT = 65535;
export const UUID_SIZE = 16;
/** Required length in bytes of a stream id; any other length is an `invalid_argument`. */
export const STREAM_ID_SIZE = 8;

/**
 * The 16-byte UUID that identifies a seimark message inside a
 * `user_data_unregistered` SEI, distinguishing it from any other vendor's.
 */
export const FORMAT_UUID: Readonly<Uint8Array> = Uint8Array.of(
  0x44, 0xa7, 0x3c, 0xb9, 0xb3, 0x6c, 0x45, 0x9a,
  0x8f, 0x1a, 0xa3, 0xaa, 0x43, 0x1f, 0x62, 0x4a,
);

/**
 * Which clock reading the origin time is: `'capture'` when the frame carried a
 * capture time, otherwise `'send'`, taken as the frame is encoded.
 */
export type TimeSource = 'send' | 'capture';

/** One decoded marker: the per-frame metadata carried in a frame's own SEI. */
export interface Marker {
  /** Which clock `originTimeUs` came from. */
  timeSource: TimeSource;
  /**
   * Origin time in microseconds since the Unix epoch. A `bigint` because the
   * field is a signed 64-bit count and a `number` would lose precision at the
   * top of the range.
   */
  originTimeUs: bigint;
  /** Per-stream counter, incremented once per marked frame, wrapping at 2^32. */
  sequence: number;
  /** Eight bytes identifying the source; see `STREAM_ID_SIZE`. */
  streamId: Uint8Array;
  /** Application bytes the sender attached, or null when the payload flag is clear. */
  payload: Uint8Array | null;
}

const OFF_FLAGS = 1;
const OFF_TIME = 2;
const OFF_SEQUENCE = 10;
const OFF_STREAM_ID = 14;
const OFF_PAYLOAD_LEN = 22;
const FLAG_CAPTURE = 0b01;
const FLAG_PAYLOAD = 0b10;

const INT64_MIN = -(2n ** 63n);
const INT64_MAX = 2n ** 63n - 1n;

/** Reports whether these bytes are the seimark UUID, so a foreign SEI is left alone. */
export function isFormatUUID(uuid: Uint8Array): boolean {
  return uuid.length === UUID_SIZE && FORMAT_UUID.every((b, i) => uuid[i] === b);
}

/**
 * Decodes a marker body, the SEI payload with the UUID already stripped. Throws
 * `SeimarkError` with `truncated` if the body is short and
 * `unsupported_version` if it is not version 1.
 */
export function decodeMarker(body: Uint8Array): Marker {
  if (body.length < FIXED_SIZE) {
    throw new SeimarkError('truncated', `marker body is ${body.length} bytes, needs ${FIXED_SIZE}`);
  }

  const version = body[0]!;
  if (version !== VERSION) {
    throw new SeimarkError('unsupported_version', `marker version ${version}`);
  }

  const view = new DataView(body.buffer, body.byteOffset, body.byteLength);
  const flags = body[OFF_FLAGS]!;

  let payload: Uint8Array | null = null;
  if ((flags & FLAG_PAYLOAD) !== 0) {
    if (body.length < OFF_PAYLOAD_LEN + 2) {
      throw new SeimarkError('truncated', 'payload flag set without a length');
    }
    const n = view.getUint16(OFF_PAYLOAD_LEN);
    const start = OFF_PAYLOAD_LEN + 2;
    if (body.length < start + n) {
      throw new SeimarkError('truncated', `payload length ${n} overruns the body`);
    }
    payload = body.slice(start, start + n);
  }

  return {
    timeSource: (flags & FLAG_CAPTURE) !== 0 ? 'capture' : 'send',
    originTimeUs: view.getBigInt64(OFF_TIME),
    sequence: view.getUint32(OFF_SEQUENCE),
    streamId: body.slice(OFF_STREAM_ID, OFF_STREAM_ID + STREAM_ID_SIZE),
    payload,
  };
}

/**
 * Encodes a marker body, without the UUID or the SEI wrapper. Throws
 * `SeimarkError` with `invalid_argument` if the stream id is not
 * `STREAM_ID_SIZE` bytes or the origin time is outside the signed 64-bit range,
 * and `payload_too_large` above `PAYLOAD_HARD_LIMIT`.
 */
export function encodeMarker(m: Marker): Uint8Array {
  if (m.streamId.length !== STREAM_ID_SIZE) {
    throw new SeimarkError('invalid_argument', `stream id is ${m.streamId.length} bytes, needs ${STREAM_ID_SIZE}`);
  }
  if (m.originTimeUs < INT64_MIN || m.originTimeUs > INT64_MAX) {
    throw new SeimarkError('invalid_argument', 'origin time is outside the signed 64-bit range');
  }
  if (m.payload !== null && m.payload.length > PAYLOAD_HARD_LIMIT) {
    throw new SeimarkError('payload_too_large', `payload is ${m.payload.length} bytes`);
  }

  const size = m.payload === null ? FIXED_SIZE : OFF_PAYLOAD_LEN + 2 + m.payload.length;
  const out = new Uint8Array(size);
  const view = new DataView(out.buffer);

  out[0] = VERSION;
  out[OFF_FLAGS] =
    (m.timeSource === 'capture' ? FLAG_CAPTURE : 0) | (m.payload === null ? 0 : FLAG_PAYLOAD);
  view.setBigInt64(OFF_TIME, m.originTimeUs);
  view.setUint32(OFF_SEQUENCE, m.sequence >>> 0);
  out.set(m.streamId, OFF_STREAM_ID);

  if (m.payload !== null) {
    view.setUint16(OFF_PAYLOAD_LEN, m.payload.length);
    out.set(m.payload, OFF_PAYLOAD_LEN + 2);
  }

  return out;
}
