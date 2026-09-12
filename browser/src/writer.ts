import { SeimarkError } from './errors.ts';
import { type Framing, joinNALUnits, nalUnits } from './nal.ts';
import {
  type TimeSource,
  PAYLOAD_SOFT_LIMIT,
  STREAM_ID_SIZE,
  encodeMarker,
} from './marker.ts';
import { buildMarkerNAL } from './sei.ts';
import { firstVCLIndex, isMarkerSEI } from './scan.ts';

// The core build excludes DOM; Web Crypto is a global in browsers and Node 22+.
declare const crypto: { getRandomValues<T extends Uint8Array>(a: T): T };

export interface WriterOptions {
  streamId?: Uint8Array;
  keyframesOnly?: boolean;
  timeSource?: TimeSource;
}

export interface MarkResult {
  data: Uint8Array;
  marked: boolean;
  warning: SeimarkError | null;
}

export class Writer {
  readonly streamId: Uint8Array;

  private readonly keyframesOnly: boolean;
  private readonly timeSource: TimeSource;
  private seq = 0;

  constructor(opts: WriterOptions = {}) {
    if (opts.streamId !== undefined && opts.streamId.length !== STREAM_ID_SIZE) {
      throw new SeimarkError('truncated', `stream id is ${opts.streamId.length} bytes, needs ${STREAM_ID_SIZE}`);
    }
    this.streamId = opts.streamId ?? crypto.getRandomValues(new Uint8Array(STREAM_ID_SIZE));
    this.keyframesOnly = opts.keyframesOnly ?? false;
    this.timeSource = opts.timeSource ?? 'send';
  }

  get sequence(): number {
    return this.seq;
  }

  mark(
    au: Uint8Array,
    framing: Framing,
    atUs: bigint,
    isKeyframe: boolean,
    payload: Uint8Array | null = null,
  ): MarkResult {
    const units = nalUnits(au, framing);

    if (units.some((u) => isMarkerSEI(u.data))) {
      throw new SeimarkError('already_marked', 'access unit already carries a marker');
    }

    if (this.keyframesOnly && !isKeyframe) {
      return { data: joinNALUnits(units, framing), marked: false, warning: null };
    }

    const at = firstVCLIndex(units);

    const body = encodeMarker({
      timeSource: this.timeSource,
      originTimeUs: atUs,
      sequence: this.seq,
      streamId: this.streamId,
      payload,
    });

    units.splice(at, 0, { data: buildMarkerNAL(body), startCodeLength: 4 });
    this.seq = (this.seq + 1) >>> 0;

    const warning =
      payload !== null && payload.length > PAYLOAD_SOFT_LIMIT
        ? new SeimarkError('payload_above_soft_limit', `payload is ${payload.length} bytes`)
        : null;

    return { data: joinNALUnits(units, framing), marked: true, warning };
  }
}
