// key/value rows <-> map. pure -> unit tested.
export type KV = { key: string; value: string }[];

export function toRows(m: Record<string, string> | undefined): KV {
  return Object.entries(m ?? {}).map(([key, value]) => ({ key, value }));
}

// trims, drops rows with empty key or value. later duplicate key wins.
export function toMap(rows: KV): Record<string, string> {
  const out: Record<string, string> = {};
  for (const r of rows) {
    const k = r.key.trim();
    const v = r.value.trim();
    if (k && v) out[k] = v;
  }
  return out;
}
