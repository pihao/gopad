// Operational-transformation primitives, mirroring the Go implementation in
// internal/ot. All lengths count Unicode code points; conversion to and from
// CodeMirror's UTF-16 offsets happens in conversion.ts.
//
// Wire format (rustpad's): a JSON array where a positive integer is a retain,
// a negative integer is a delete, and a string is an insert.

export type Comp = number | string;

export function cpLen(s: string): number {
  let n = 0;
  for (let i = 0; i < s.length; i++) {
    const code = s.charCodeAt(i);
    if (code >= 0xd800 && code < 0xdc00) i++;
    n++;
  }
  return n;
}

/** Drop the first n code points of s. */
function cpSliceFrom(s: string, n: number): string {
  let i = 0;
  while (n-- > 0) i += s.codePointAt(i)! > 0xffff ? 2 : 1;
  return s.slice(i);
}

/** Keep only the first n code points of s. */
function cpSliceTo(s: string, n: number): string {
  let i = 0;
  while (n-- > 0) i += s.codePointAt(i)! > 0xffff ? 2 : 1;
  return s.slice(0, i);
}

export class Operation {
  ops: Comp[] = [];
  baseLen = 0;
  targetLen = 0;

  static fromJSON(arr: unknown): Operation {
    if (!Array.isArray(arr)) throw new Error("ot: operation must be an array");
    const op = new Operation();
    for (const c of arr) {
      if (typeof c === "string") op.insert(c);
      else if (typeof c === "number" && Number.isInteger(c)) {
        if (c >= 0) op.retain(c);
        else op.delete(-c);
      } else throw new Error(`ot: invalid component ${JSON.stringify(c)}`);
    }
    return op;
  }

  /** JSON.stringify picks this up automatically. */
  toJSON(): Comp[] {
    return this.ops;
  }

  isNoop(): boolean {
    return this.ops.length === 0 || (this.ops.length === 1 && typeof this.ops[0] === "number" && this.ops[0] > 0);
  }

  retain(n: number): this {
    if (n <= 0) return this;
    this.baseLen += n;
    this.targetLen += n;
    const last = this.ops[this.ops.length - 1];
    if (typeof last === "number" && last > 0) this.ops[this.ops.length - 1] = last + n;
    else this.ops.push(n);
    return this;
  }

  delete(n: number): this {
    if (n <= 0) return this;
    this.baseLen += n;
    const last = this.ops[this.ops.length - 1];
    if (typeof last === "number" && last < 0) this.ops[this.ops.length - 1] = last - n;
    else this.ops.push(-n);
    return this;
  }

  insert(s: string): this {
    if (s === "") return this;
    this.targetLen += cpLen(s);
    const ops = this.ops;
    const last = ops[ops.length - 1];
    if (typeof last === "string") {
      ops[ops.length - 1] = last + s;
    } else if (typeof last === "number" && last < 0) {
      // Canonical order: insert before an adjacent delete.
      const prev = ops[ops.length - 2];
      if (typeof prev === "string") ops[ops.length - 2] = prev + s;
      else ops.splice(ops.length - 1, 0, s);
    } else {
      ops.push(s);
    }
    return this;
  }

  apply(doc: string): string {
    const chars = [...doc];
    if (chars.length !== this.baseLen) {
      throw new Error(`ot: apply: operation base length ${this.baseLen} does not match document length ${chars.length}`);
    }
    let out = "";
    let pos = 0;
    for (const c of this.ops) {
      if (typeof c === "string") out += c;
      else if (c > 0) {
        out += chars.slice(pos, pos + c).join("");
        pos += c;
      } else pos += -c;
    }
    return out;
  }

  /** Map a cursor position (code points) through this operation. */
  transformIndex(pos: number): number {
    let index = pos;
    let newIndex = pos;
    for (const c of this.ops) {
      if (typeof c === "string") newIndex += cpLen(c);
      else if (c > 0) index -= c;
      else {
        newIndex -= Math.min(index, -c);
        index -= -c;
      }
      if (index < 0) break;
    }
    return newIndex;
  }

  /** Compose two consecutive operations: apply(apply(doc, a), b) == apply(doc, compose(a, b)). */
  static compose(a: Operation, b: Operation): Operation {
    if (a.targetLen !== b.baseLen) {
      throw new Error(`ot: compose: target length ${a.targetLen} != base length ${b.baseLen}`);
    }
    const out = new Operation();
    let i = 0;
    let j = 0;
    let op1: Comp | undefined = a.ops[i++];
    let op2: Comp | undefined = b.ops[j++];
    for (;;) {
      if (op1 === undefined && op2 === undefined) break;
      if (typeof op1 === "number" && op1 < 0) {
        out.delete(-op1);
        op1 = a.ops[i++];
        continue;
      }
      if (typeof op2 === "string") {
        out.insert(op2);
        op2 = b.ops[j++];
        continue;
      }
      if (op1 === undefined || op2 === undefined) throw new Error("ot: compose: operations do not line up");
      if (typeof op1 === "number" && typeof op2 === "number" && op1 > 0 && op2 > 0) {
        const n = Math.min(op1, op2);
        out.retain(n);
        op1 -= n;
        op2 -= n;
        if (op1 === 0) op1 = a.ops[i++];
        if (op2 === 0) op2 = b.ops[j++];
      } else if (typeof op1 === "string" && typeof op2 === "number" && op2 < 0) {
        const n = Math.min(cpLen(op1), -op2);
        op1 = cpSliceFrom(op1, n);
        op2 += n;
        if (op1 === "") op1 = a.ops[i++];
        if (op2 === 0) op2 = b.ops[j++];
      } else if (typeof op1 === "string" && typeof op2 === "number" && op2 > 0) {
        const n = Math.min(cpLen(op1), op2);
        out.insert(cpSliceTo(op1, n));
        op1 = cpSliceFrom(op1, n);
        op2 -= n;
        if (op1 === "") op1 = a.ops[i++];
        if (op2 === 0) op2 = b.ops[j++];
      } else if (typeof op1 === "number" && op1 > 0 && typeof op2 === "number" && op2 < 0) {
        const n = Math.min(op1, -op2);
        out.delete(n);
        op1 -= n;
        op2 += n;
        if (op1 === 0) op1 = a.ops[i++];
        if (op2 === 0) op2 = b.ops[j++];
      } else {
        throw new Error("ot: compose: operations do not line up");
      }
    }
    return out;
  }

  /**
   * Transform two concurrent operations into [a', b'] such that applying
   * a then b' equals applying b then a'. Ties between concurrent inserts go
   * to a. Matches the server: the server calls transform(incoming, history).
   */
  static transform(a: Operation, b: Operation): [Operation, Operation] {
    if (a.baseLen !== b.baseLen) {
      throw new Error(`ot: transform: different base lengths ${a.baseLen} and ${b.baseLen}`);
    }
    const ap = new Operation();
    const bp = new Operation();
    let i = 0;
    let j = 0;
    let op1: Comp | undefined = a.ops[i++];
    let op2: Comp | undefined = b.ops[j++];
    while (op1 !== undefined || op2 !== undefined) {
      if (typeof op1 === "string") {
        ap.insert(op1);
        bp.retain(cpLen(op1));
        op1 = a.ops[i++];
        continue;
      }
      if (typeof op2 === "string") {
        ap.retain(cpLen(op2));
        bp.insert(op2);
        op2 = b.ops[j++];
        continue;
      }
      if (op1 === undefined || op2 === undefined) throw new Error("ot: transform: operations do not line up");
      let n: number;
      if (op1 > 0 && op2 > 0) {
        n = Math.min(op1, op2);
        ap.retain(n);
        bp.retain(n);
        op1 -= n;
        op2 -= n;
      } else if (op1 < 0 && op2 < 0) {
        n = Math.min(-op1, -op2);
        op1 += n;
        op2 += n;
      } else if (op1 < 0 && op2 > 0) {
        n = Math.min(-op1, op2);
        ap.delete(n);
        op1 += n;
        op2 -= n;
      } else if (op1 > 0 && op2 < 0) {
        n = Math.min(op1, -op2);
        bp.delete(n);
        op1 -= n;
        op2 += n;
      } else {
        throw new Error("ot: transform: operations do not line up");
      }
      if (op1 === 0) op1 = a.ops[i++];
      if (op2 === 0) op2 = b.ops[j++];
    }
    return [ap, bp];
  }
}
