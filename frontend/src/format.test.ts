import { describe, expect, it } from "vitest";
import { fmtDate, fmtRelative } from "./format";

describe("fmtDate", () => {
  it("formats as 2006-01-02 15:04:05 +06:00", () => {
    expect(fmtDate(1755155085)).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} [+-]\d{2}:\d{2}$/);
  });

  it("renders zero as a dash", () => {
    expect(fmtDate(0)).toBe("-");
  });
});

describe("fmtRelative", () => {
  const now = 1_755_000_000_000; // fixed reference point in ms

  it("picks sensible units across magnitudes", () => {
    const base = now / 1000;
    expect(fmtRelative(base + 30, now)).toBe("in 30 seconds");
    expect(fmtRelative(base + 5 * 60, now)).toBe("in 5 minutes");
    expect(fmtRelative(base + 23 * 3600, now)).toBe("in 23 hours");
    expect(fmtRelative(base + 7 * 86400, now)).toBe("in 7 days");
    expect(fmtRelative(base + 100 * 365 * 86400, now)).toBe("in 100 years");
  });

  it("handles the past and zero", () => {
    const base = now / 1000;
    expect(fmtRelative(base - 3 * 3600, now)).toBe("3 hours ago");
    expect(fmtRelative(0, now)).toBe("-");
  });
});
