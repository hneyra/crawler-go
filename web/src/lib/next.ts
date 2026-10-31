// next= must stay on this origin, path only. open redirect guard. pure -> unit tested.
export function safeNext(raw: string | null | undefined, fallback = '/'): string {
  if (!raw || !raw.startsWith('/') || raw.startsWith('//') || raw.startsWith('/\\'))
    return fallback;
  return raw;
}
