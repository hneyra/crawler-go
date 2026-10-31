// next= must stay on this origin. open redirect guard. pure -> unit tested.
// resolve like the browser would (tabs/newlines stripped, backslashes = slashes) then compare origin.
export function safeNext(
  raw: string | null | undefined,
  fallback = '/',
  origin = 'http://app.invalid'
): string {
  if (!raw || !raw.startsWith('/')) return fallback;
  let u: URL;
  try {
    u = new URL(raw, origin);
  } catch {
    return fallback;
  }
  if (u.origin !== origin) return fallback;
  return u.pathname + u.search + u.hash;
}
