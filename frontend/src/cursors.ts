// Remote cursor and selection rendering as a CodeMirror extension.
// Positions arrive here already converted to UTF-16 offsets.
import { StateEffect, StateField } from "@codemirror/state";
import type { Range, Text } from "@codemirror/state";
import { Decoration, EditorView, WidgetType } from "@codemirror/view";
import type { DecorationSet } from "@codemirror/view";

import { caretColor, labelBackground, selectionBackground } from "./colors";

export interface RemoteCursorSet {
  id: number;
  name: string;
  hue: number;
  /** Bumped on every cursor update; a change replays the label fade-out. */
  stamp: number;
  cursors: number[];
  selections: [number, number][];
}

export const setRemoteCursors = StateEffect.define<RemoteCursorSet[]>();

class CaretWidget extends WidgetType {
  constructor(
    readonly hue: number,
    readonly name: string,
    readonly stamp: number,
    /** First-line carets show the label below: above it would be clipped. */
    readonly below: boolean,
  ) {
    super();
  }

  eq(other: CaretWidget): boolean {
    // A different stamp forces the DOM to be rebuilt, restarting the
    // show-then-fade animation of the name label.
    return (
      other.hue === this.hue &&
      other.name === this.name &&
      other.stamp === this.stamp &&
      other.below === this.below
    );
  }

  toDOM(): HTMLElement {
    const caret = document.createElement("span");
    caret.className = "remote-caret";
    caret.style.borderLeftColor = caretColor(this.hue);
    const label = document.createElement("span");
    label.className = this.below ? "remote-caret-label flash below" : "remote-caret-label flash";
    label.textContent = this.name;
    label.style.backgroundColor = labelBackground(this.hue);
    caret.appendChild(label);
    return caret;
  }

  override ignoreEvent(): boolean {
    return true;
  }
}

function buildDecorations(users: RemoteCursorSet[], doc: Text): DecorationSet {
  const ranges: Range<Decoration>[] = [];
  const clamp = (p: number) => Math.min(Math.max(p, 0), doc.length);
  for (const u of users) {
    for (const sel of u.selections) {
      const from = clamp(Math.min(sel[0], sel[1]));
      const to = clamp(Math.max(sel[0], sel[1]));
      if (from < to) {
        ranges.push(
          Decoration.mark({
            class: "remote-selection",
            attributes: { style: `background-color: ${selectionBackground(u.hue)}` },
          }).range(from, to),
        );
      }
    }
    for (const pos of u.cursors) {
      const p = clamp(pos);
      const below = doc.lineAt(p).number === 1;
      ranges.push(Decoration.widget({ widget: new CaretWidget(u.hue, u.name, u.stamp, below), side: 0 }).range(p));
    }
  }
  return Decoration.set(ranges, true);
}

const remoteCursorField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(deco, tr) {
    deco = deco.map(tr.changes);
    for (const e of tr.effects) {
      if (e.is(setRemoteCursors)) deco = buildDecorations(e.value, tr.newDoc);
    }
    return deco;
  },
  provide: (f) => EditorView.decorations.from(f),
});

export const remoteCursorExtension = [remoteCursorField];
