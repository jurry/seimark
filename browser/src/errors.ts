export type SeimarkErrorCode =
  | 'unsupported_version'
  | 'truncated'
  | 'payload_too_large'
  | 'payload_above_soft_limit'
  | 'unparsable_sei'
  | 'no_vcl'
  | 'already_marked';

export class SeimarkError extends Error {
  readonly code: SeimarkErrorCode;

  constructor(code: SeimarkErrorCode, message: string) {
    super(message);
    this.name = 'SeimarkError';
    this.code = code;
  }
}
