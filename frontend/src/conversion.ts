// Conversion between CodeMirror's UTF-16 world and the protocol's
// code-point world.
import type { ChangeSet, ChangeSpec, Text } from "@codemirror/state";
import { cpLen, Operation } from "./ot";

/** UTF-16 offset of the cp-th code point in text. */
export function cpToUtf16(text: string, cp: number): number {
  let i = 0;
  while (cp > 0 && i < text.length) {
    i += text.codePointAt(i)! > 0xffff ? 2 : 1;
    cp--;
  }
  return i;
}

/** Number of code points before the UTF-16 offset off in text. */
export function utf16ToCp(text: string, off: number): number {
  let n = 0;
  let i = 0;
  while (i < off && i < text.length) {
    i += text.codePointAt(i)! > 0xffff ? 2 : 1;
    n++;
  }
  return n;
}

/** Convert a CodeMirror ChangeSet (against startDoc) into an Operation. */
export function changesToOperation(startDoc: Text, changes: ChangeSet): Operation {
  const op = new Operation();
  let pos = 0;
  changes.iterChanges((fromA, toA, _fromB, _toB, inserted) => {
    op.retain(cpLen(startDoc.sliceString(pos, fromA)));
    op.delete(cpLen(startDoc.sliceString(fromA, toA)));
    op.insert(inserted.toString());
    pos = toA;
  });
  op.retain(cpLen(startDoc.sliceString(pos)));
  return op;
}

/** Convert an Operation into CodeMirror change specs against docText. */
export function operationToChanges(docText: string, op: Operation): ChangeSpec[] {
  const specs: ChangeSpec[] = [];
  let idx = 0; // UTF-16 offset into docText
  const advance = (cp: number): number => {
    let end = idx;
    while (cp-- > 0) end += docText.codePointAt(end)! > 0xffff ? 2 : 1;
    return end;
  };
  for (const c of op.ops) {
    if (typeof c === "string") {
      specs.push({ from: idx, insert: c });
    } else if (c > 0) {
      idx = advance(c);
    } else {
      const end = advance(-c);
      specs.push({ from: idx, to: end });
      idx = end;
    }
  }
  return specs;
}
