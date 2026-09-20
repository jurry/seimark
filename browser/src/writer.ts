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

/** Settings fixed for the life of a `Writer`; all of them have a default. */
export interface WriterOptions {
  /**
   * Eight bytes identifying the source. Defaults to eight random bytes from Web
   * Crypto; any other length throws `invalid_argument`.
   */
  streamId?: Uint8Array;
  /** Mark only keyframes instead of every access unit. Defaults to false. */
  keyframesOnly?: boolean;
  /** Which clock the caller's times come from. Defaults to `'send'`. */
  timeSource?: TimeSource;
}

/** What one call to `Writer.mark` produced. */
export interface MarkResult {
  /** The access unit, with the marker SEI inserted when `marked` is true. */
  data: Uint8Array;
  /** False when `keyframesOnly` skipped this access unit; no sequence was spent. */
  marked: boolean;
  /**
   * A `payload_above_soft_limit` warning when the payload exceeds
   * `PAYLOAD_SOFT_LIMIT`. The marker was still written.
   */
  warning: SeimarkError | null;
}

/**
 * Writes a marker into each access unit it is given, counting the sequence.
 * One writer per stream: the sequence and the stream id belong to it.
 */
export class Writer {
  /** The id this writer stamps, whether given or randomly generated. */
  readonly streamId: Uint8Array;

  private readonly keyframesOnly: boolean;
  private readonly timeSource: TimeSource;
  private seq = 0;

  constructor(opts: WriterOptions = {}) {
    if (opts.streamId !== undefined && opts.streamId.length !== STREAM_ID_SIZE) {
      throw new SeimarkError('invalid_argument', `stream id is ${opts.streamId.length} bytes, needs ${STREAM_ID_SIZE}`);
    }
    this.streamId = opts.streamId ?? crypto.getRandomValues(new Uint8Array(STREAM_ID_SIZE));
    this.keyframesOnly = opts.keyframesOnly ?? false;
    this.timeSource = opts.timeSource ?? 'send';
  }

  /** The sequence the next marked access unit will carry; wraps at 2^32. */
  get sequence(): number {
    return this.seq;
  }

  /**
   * Inserts a marker before the first VCL NAL unit of `au` and advances the
   * sequence. `atUs` is the origin time in microseconds since the Unix epoch.
   * Throws `SeimarkError` with `already_marked` if the access unit already
   * carries one, and `no_vcl` if it has no slice to sit in front of.
   */
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
