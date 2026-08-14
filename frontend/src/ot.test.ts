import { describe, expect, it } from "vitest";
import { cpLen, Operation } from "./ot";
import { cpToUtf16, utf16ToCp } from "./conversion";

// mulberry32: tiny seeded PRNG so failures are reproducible.
function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const alphabet = [..."abcXYZ 0Ωé中文字🌍😀\n"];

function randDoc(rng: () => number, maxLen: number): string {
  const n = Math.floor(rng() * (maxLen + 1));
  let s = "";
  for (let i = 0; i < n; i++) s += alphabet[Math.floor(rng() * alphabet.length)];
  return s;
}

function randText(rng: () => number): string {
  const n = 1 + Math.floor(rng() * 4);
  let s = "";
  for (let i = 0; i < n; i++) s += alphabet[Math.floor(rng() * alphabet.length)];
  return s;
}

function randOp(rng: () => number, doc: string): Operation {
  const op = new Operation();
  let remaining = cpLen(doc);
  while (remaining > 0) {
    const n = 1 + Math.floor(rng() * remaining);
    const kind = Math.floor(rng() * 3);
    if (kind === 0) {
      op.retain(n);
      remaining -= n;
    } else if (kind === 1) {
      op.delete(n);
      remaining -= n;
    } else {
      op.insert(randText(rng));
    }
  }
  if (rng() < 0.5) op.insert(randText(rng));
  return op;
}

describe("Operation", () => {
  it("applies inserts and deletes by code points", () => {
    const op = new Operation().retain(5).delete(1).insert("🌍世界");
    expect(op.apply("héllo😀")).toBe("héllo🌍世界");
    expect(op.baseLen).toBe(6);
    expect(op.targetLen).toBe(8);
  });

  it("canonicalizes insert-after-delete", () => {
    const a = new Operation().retain(1).delete(2).insert("x");
    const b = new Operation().retain(1).insert("x").delete(2);
    expect(a.ops).toEqual(b.ops);
  });

  it("serializes to the rustpad wire format", () => {
    const op = new Operation().retain(5).insert(" world").delete(2);
    expect(JSON.stringify(op)).toBe('[5," world",-2]');
    expect(Operation.fromJSON(JSON.parse('[5," world",-2]')).ops).toEqual(op.ops);
  });

  it("transform converges (TP1) on random operation pairs", () => {
    for (let seed = 1; seed <= 500; seed++) {
      const rng = mulberry32(seed);
      const doc = randDoc(rng, 30);
      const a = randOp(rng, doc);
      const b = randOp(rng, doc);
      const [a2, b2] = Operation.transform(a, b);
      const viaA = b2.apply(a.apply(doc));
      const viaB = a2.apply(b.apply(doc));
      expect(viaA, `seed ${seed}`).toBe(viaB);
    }
  });

  it("compose is equivalent to sequential application", () => {
    for (let seed = 1; seed <= 500; seed++) {
      const rng = mulberry32(seed);
      const doc = randDoc(rng, 30);
      const a = randOp(rng, doc);
      const docA = a.apply(doc);
      const b = randOp(rng, docA);
      expect(Operation.compose(a, b).apply(doc), `seed ${seed}`).toBe(b.apply(docA));
    }
  });

  it("transformIndex clamps into the target document", () => {
    const op = new Operation().delete(2).retain(3);
    expect(op.transformIndex(1)).toBe(0);
    expect(op.transformIndex(4)).toBe(2);
  });
});

describe("offset conversion", () => {
  it("roundtrips UTF-16 and code point offsets", () => {
    const text = "a😀b中🌍c";
    for (let cp = 0; cp <= cpLen(text); cp++) {
      expect(utf16ToCp(text, cpToUtf16(text, cp))).toBe(cp);
    }
  });
});
