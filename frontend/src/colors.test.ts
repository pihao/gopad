import { describe, expect, it } from "vitest";
import { HUES, MIN_HUE_DISTANCE, caretColor, nameColor, randomHue } from "./colors";

function circularDistance(a: number, b: number): number {
  const d = Math.abs(a - b);
  return Math.min(d, 360 - d);
}

describe("randomHue", () => {
  it("always picks from the pool", () => {
    for (let i = 0; i < 100; i++) {
      expect(HUES).toContain(randomHue());
    }
  });

  it("keeps a minimum distance from excluded hues", () => {
    const exclude = [0, 120, 235];
    for (let i = 0; i < 100; i++) {
      const h = randomHue(exclude);
      for (const e of exclude) {
        expect(circularDistance(h, e)).toBeGreaterThanOrEqual(MIN_HUE_DISTANCE);
      }
    }
  });

  it("never returns the caller's current hue when excluded", () => {
    for (const current of HUES) {
      for (let i = 0; i < 20; i++) {
        expect(randomHue([current])).not.toBe(current);
      }
    }
  });

  it("treats exclusion circularly across 0/360", () => {
    // 355 is within MIN_HUE_DISTANCE of pool hue 0 going through 360.
    for (let i = 0; i < 50; i++) {
      expect(randomHue([355])).not.toBe(0);
    }
  });

  it("falls back to the farthest pool hue when everything is excluded", () => {
    // Every pool hue is excluded, but 170 only by a nearby hue (10° off),
    // so the fallback must pick it as the farthest from the excluded set.
    const exclude = HUES.map((h) => (h === 170 ? 160 : h));
    expect(randomHue(exclude)).toBe(170);
  });
});

describe("color derivation", () => {
  it("applies the pool entry's lightness calibration", () => {
    expect(caretColor(235)).toBe("hsl(235, 90%, 62%)"); // blue lifted
    expect(caretColor(50)).toBe("hsl(50, 90%, 49%)"); // amber dimmed
    expect(nameColor(0)).toBe("hsl(0, 90%, 65%)"); // red unshifted
  });

  it("gives arbitrary legacy hues the nearest entry's calibration", () => {
    expect(caretColor(240)).toBe("hsl(240, 90%, 62%)"); // nearest: blue 235
    expect(nameColor(58)).toBe("hsl(58, 90%, 59%)"); // nearest: amber 50
  });
});
