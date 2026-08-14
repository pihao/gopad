// Date formatting shared by the editor and admin pages.

const pad = (n: number): string => String(n).padStart(2, "0");

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
