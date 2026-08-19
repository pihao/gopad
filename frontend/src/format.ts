// Date formatting shared by the editor and admin pages.

const pad = (n: number): string => String(n).padStart(2, "0");

const relFmt = new Intl.RelativeTimeFormat("en", { numeric: "always" });

/** Format unix seconds as a relative time like "in 23 hours". */
export function fmtRelative(unixSeconds: number, nowMs = Date.now()): string {
  if (!unixSeconds) return "-";
  const diffSec = Math.round(unixSeconds - nowMs / 1000);
  const abs = Math.abs(diffSec);
  if (abs < 60) return relFmt.format(diffSec, "seconds");
  if (abs < 3600) return relFmt.format(Math.round(diffSec / 60), "minutes");
  if (abs < 48 * 3600) return relFmt.format(Math.round(diffSec / 3600), "hours");
  if (abs < 365 * 86400) return relFmt.format(Math.round(diffSec / 86400), "days");
  return relFmt.format(Math.round(diffSec / (365 * 86400)), "years");
}

/** Format unix seconds as `2006-01-02 15:04:05 +06:00` (local time). */
export function fmtDate(unixSeconds: number): string {
  if (!unixSeconds) return "-";
  const d = new Date(unixSeconds * 1000);
  const offMin = -d.getTimezoneOffset(); // minutes east of UTC
  const sign = offMin >= 0 ? "+" : "-";
  const abs = Math.abs(offMin);
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())} ` +
    `${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`
  );
}

/** Format unix seconds as RFC 3339, `2006-01-02T15:04:05+06:00` (local time). */
export function fmtRFC3339(unixSeconds: number): string {
  if (!unixSeconds) return "-";
  const d = new Date(unixSeconds * 1000);
  const offMin = -d.getTimezoneOffset(); // minutes east of UTC
  const sign = offMin >= 0 ? "+" : "-";
  const abs = Math.abs(offMin);
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}` +
    `${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`
  );
}
