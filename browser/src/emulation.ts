export function addEmulationPrevention(rbsp: Uint8Array): Uint8Array {
  const out: number[] = [];
  let zeros = 0;

  for (const b of rbsp) {
    if (zeros === 2 && b <= 0x03) {
      out.push(0x03);
      zeros = 0;
    }
    out.push(b);
    zeros = b === 0x00 ? zeros + 1 : 0;
  }

  return Uint8Array.from(out);
}

export function removeEmulationPrevention(nal: Uint8Array): Uint8Array {
  const out = new Uint8Array(nal.length);
  let n = 0;
  let zeros = 0;

  for (const b of nal) {
    if (zeros === 2 && b === 0x03) {
      zeros = 0;
      continue;
    }
    out[n++] = b;
    zeros = b === 0x00 ? zeros + 1 : 0;
  }

  return out.subarray(0, n);
}
