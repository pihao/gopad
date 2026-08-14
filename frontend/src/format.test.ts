import { describe, expect, it } from "vitest";
import { fmtDate } from "./format";

describe("fmtDate", () => {
  it("formats as 2006-01-02 15:04:05 +06:00", () => {
    expect(fmtDate(1755155085)).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} [+-]\d{2}:\d{2}$/);
  });

  it("renders zero as a dash", () => {
    expect(fmtDate(0)).toBe("-");
  });
});
