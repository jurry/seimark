import { type SeimarkErrorCode, SeimarkError } from '../errors.ts';
import { type Marker, type TimeSource, PAYLOAD_SOFT_LIMIT } from '../marker.ts';
import { detectFraming } from '../nal.ts';
import { Writer } from '../writer.ts';

export interface FrameLike {
  data: ArrayBuffer;
  type?: 'key' | 'delta' | 'empty';
  getMetadata(): { captureTime?: number };
}

export interface HandlerOptions {
  writer: Writer;
  keyframesOnly: boolean;
  nowUs: () => bigint;
  captureUs: (captureTime: number) => bigint;
  onWarn: (code: SeimarkErrorCode, message: string, frames: number) => void;
}

export interface HandlerStats {
  framesSeen: number;
  framesMarked: number;
  errors: number;
}

export class FrameHandler {
  passthrough = false;
  lastMarker: Marker | null = null;
  readonly stats: HandlerStats = { framesSeen: 0, framesMarked: 0, errors: 0 };

  private payload: Uint8Array | null = null;
  private timeSource: TimeSource = 'send';
  private probed = false;
  private warned = false;

  private writer: Writer;
  private readonly opts: HandlerOptions;

  constructor(opts: HandlerOptions) {
    this.opts = opts;
    this.writer = opts.writer;
  }

  /** The verdict is taken from the first frame only; later frames never change it. */
  probe(frame: FrameLike): TimeSource {
    if (!this.probed) {
      this.probed = true;
      if (frame.getMetadata().captureTime !== undefined) {
        this.timeSource = 'capture';
        this.writer = new Writer({
          streamId: this.writer.streamId,
          keyframesOnly: this.opts.keyframesOnly,
          timeSource: 'capture',
        });
      }
    }

    return this.timeSource;
  }

  setPayload(bytes: Uint8Array | null): void {
    this.payload = bytes;
    if (bytes !== null && bytes.length > PAYLOAD_SOFT_LIMIT) {
      this.opts.onWarn(
        'payload_above_soft_limit',
        `payload is ${bytes.length} bytes`,
        this.stats.framesSeen,
      );
    }
  }

  handle(frame: FrameLike): void {
    this.stats.framesSeen++;
    if (this.passthrough || frame.type === 'empty') return;

    try {
      const au = new Uint8Array(frame.data);
      const framing = detectFraming(au);
      if (framing === null) {
        throw new SeimarkError('truncated', 'frame data is in no recognised framing');
      }

      const source = this.probe(frame);
      const capture = frame.getMetadata().captureTime;
      const at =
        source === 'capture' && capture !== undefined
          ? this.opts.captureUs(capture)
          : this.opts.nowUs();

      const { data, marked } = this.writer.mark(
        au,
        framing,
        at,
        frame.type === 'key',
        this.payload,
      );

      if (marked) {
        frame.data = data.buffer.slice(
          data.byteOffset,
          data.byteOffset + data.byteLength,
        ) as ArrayBuffer;
        this.lastMarker = {
          timeSource: this.timeSource,
          originTimeUs: at,
          sequence: (this.writer.sequence - 1) >>> 0,
          streamId: this.writer.streamId,
          payload: this.payload,
        };
        this.stats.framesMarked++;
      }
    } catch (e) {
      this.stats.errors++;
      if (!this.warned) {
        this.warned = true;
        const code = e instanceof SeimarkError ? e.code : 'unparsable_sei';
        const message = e instanceof Error ? e.message : String(e);
        // onWarn is a page callback; it must not be able to throw back into the
        // encoded transform, or a single misbehaving page handler would end the call.
        try {
          this.opts.onWarn(code, message, this.stats.framesSeen);
        } catch {
          // Swallowed deliberately: see comment above.
        }
      }
    }
  }
}
