/**
 * The machine-readable reason a seimark operation failed. The code, not the
 * message, is what a caller should branch on; messages are for humans and may
 * change.
 */
export type SeimarkErrorCode =
  | 'unsupported_version'
  | 'truncated'
  | 'payload_too_large'
  | 'payload_above_soft_limit'
  | 'unparsable_sei'
  | 'no_vcl'
  | 'already_marked'
  | 'unsupported_browser'
  | 'invalid_argument'
  | 'already_attached'
  | 'csp_blocked'
  // Not raised by this package: the handler's label for a foreign exception.
  | 'unknown';

/**
 * Every error this package raises. Thrown synchronously by the codec and by
 * `attach`; on a live stream the same value is instead reported through
 * `onError` as a code and message pair, never thrown into the transform.
 */
export class SeimarkError extends Error {
  /** The reason, for branching; `message` explains the same thing for a human. */
  readonly code: SeimarkErrorCode;

  constructor(code: SeimarkErrorCode, message: string) {
    super(message);
    this.name = 'SeimarkError';
    this.code = code;
  }
}
