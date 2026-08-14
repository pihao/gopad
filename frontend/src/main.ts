import "./style.css";
import { basicSetup } from "codemirror";
import { Annotation, Compartment, EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { oneDark } from "@codemirror/theme-one-dark";

import { Connection } from "./connection";
import type { CursorData, UserInfo } from "./connection";
import { changesToOperation, cpToUtf16, operationToChanges, utf16ToCp } from "./conversion";
import { remoteCursorExtension, setRemoteCursors } from "./cursors";
import type { RemoteCursorSet } from "./cursors";
import { languageExtension, languages } from "./languages";
import { OTClient } from "./otClient";

// --- Document identity from the URL hash -----------------------------------

function randomId(): string {
  const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return [...bytes].map((b) => alphabet[b % alphabet.length]).join("");
}

function parseHash(): { id: string; readonly: boolean } {
  const h = location.hash.replace(/^#/, "");
  if (h.startsWith("view/")) return { id: h.slice("view/".length), readonly: true };
  if (h !== "") return { id: h, readonly: false };
  const id = randomId();
  history.replaceState(null, "", "#" + id);
  return { id, readonly: false };
}

const { id: docId, readonly } = parseHash();
// Navigating to a different document is handled by a full reload.
window.addEventListener("hashchange", () => location.reload());

// --- DOM handles ------------------------------------------------------------

const $ = <T extends HTMLElement>(sel: string): T => document.querySelector(sel) as T;
const statusEl = $("#status");
const bannerEl = $("#banner");
const docIdEl = $("#doc-id");
const usersEl = $<HTMLUListElement>("#users");
const nameInput = $<HTMLInputElement>("#name-input");
const meDot = $("#me-dot");
const hueBtn = $<HTMLButtonElement>("#hue-btn");
const languageSelect = $<HTMLSelectElement>("#language");
const ttlSelect = $<HTMLSelectElement>("#ttl");
const expiresAtEl = $("#expires-at");
const copyLinkBtn = $<HTMLButtonElement>("#copy-link");

docIdEl.textContent = docId;
$("#readonly-badge").hidden = !readonly;

// --- Local identity ---------------------------------------------------------

const storedName = localStorage.getItem("gopad-name");
const storedHue = Number(localStorage.getItem("gopad-hue"));
let myInfo: UserInfo = {
  name: storedName || `Guest ${1000 + Math.floor(Math.random() * 9000)}`,
  hue: Number.isFinite(storedHue) && storedHue >= 0 ? storedHue : Math.floor(Math.random() * 360),
};
nameInput.value = myInfo.name;

// --- Collaboration state ----------------------------------------------------

const users = new Map<number, UserInfo>();
const cursors = new Map<number, CursorData>();
let myId = -1;
let killed = false;

const remoteAnnotation = Annotation.define<boolean>();
const languageCompartment = new Compartment();
const readOnlyCompartment = new Compartment();

const client = new OTClient(
  (revision, op) => conn.send({ Edit: { revision, operation: op } }),
  (op) => {
    const specs = operationToChanges(view.state.doc.toString(), op);
    view.dispatch({ changes: specs, annotations: remoteAnnotation.of(true) });
  },
);

// --- Editor -----------------------------------------------------------------

let cursorTimer: number | undefined;

const view = new EditorView({
  parent: $("#editor"),
  state: EditorState.create({
    doc: "",
    extensions: [
      basicSetup,
      oneDark,
      remoteCursorExtension,
      languageCompartment.of(languageExtension("plaintext")),
      readOnlyCompartment.of(readonly ? [EditorState.readOnly.of(true), EditorView.editable.of(false)] : []),
      EditorView.updateListener.of((update) => {
        const isRemote = update.transactions.some((tr) => tr.annotation(remoteAnnotation));
        if (update.docChanged && !isRemote) {
          client.localEdit(changesToOperation(update.startState.doc, update.changes));
        }
        if ((update.selectionSet || update.docChanged) && !isRemote) {
          window.clearTimeout(cursorTimer);
          cursorTimer = window.setTimeout(broadcastCursor, 80);
        }
        if (update.docChanged) refreshRemoteCursors();
      }),
    ],
  }),
});

function broadcastCursor(): void {
  if (killed) return;
  const text = view.state.doc.toString();
  const data: CursorData = { cursors: [], selections: [] };
  for (const r of view.state.selection.ranges) {
    data.cursors.push(utf16ToCp(text, r.head));
    if (!r.empty) data.selections.push([utf16ToCp(text, r.from), utf16ToCp(text, r.to)]);
  }
  conn.send({ CursorData: data });
}

function refreshRemoteCursors(): void {
  const text = view.state.doc.toString();
  const sets: RemoteCursorSet[] = [];
  for (const [id, data] of cursors) {
    if (id === myId) continue;
    const info = users.get(id);
    if (!info) continue;
    sets.push({
      id,
      name: info.name,
      hue: info.hue,
      cursors: data.cursors.map((c) => cpToUtf16(text, c)),
      selections: data.selections.map(([a, b]) => [cpToUtf16(text, a), cpToUtf16(text, b)] as [number, number]),
    });
  }
  view.dispatch({ effects: setRemoteCursors.of(sets) });
}

// --- Sidebar ----------------------------------------------------------------

function renderMe(): void {
  meDot.style.backgroundColor = `hsl(${myInfo.hue}, 90%, 55%)`;
}

function renderUsers(): void {
  usersEl.replaceChildren();
  const entries = [...users.entries()].sort((a, b) => a[0] - b[0]);
  for (const [id, info] of entries) {
    const li = document.createElement("li");
    const dot = document.createElement("span");
    dot.className = "dot";
    dot.style.backgroundColor = `hsl(${info.hue}, 90%, 55%)`;
    const name = document.createElement("span");
    name.textContent = id === myId ? `${info.name} (you)` : info.name;
    li.append(dot, name);
    usersEl.appendChild(li);
  }
}

function sendMyInfo(): void {
  localStorage.setItem("gopad-name", myInfo.name);
  localStorage.setItem("gopad-hue", String(myInfo.hue));
  renderMe();
  conn.send({ ClientInfo: myInfo });
}

nameInput.addEventListener("change", () => {
  const name = nameInput.value.trim();
  if (name) {
    myInfo = { ...myInfo, name };
    sendMyInfo();
  }
});

hueBtn.addEventListener("click", () => {
  myInfo = { ...myInfo, hue: Math.floor(Math.random() * 360) };
  sendMyInfo();
});

for (const name of Object.keys(languages)) {
  const opt = document.createElement("option");
  opt.value = name;
  opt.textContent = name;
  languageSelect.appendChild(opt);
}
languageSelect.value = "plaintext";
languageSelect.disabled = readonly;
languageSelect.addEventListener("change", () => {
  conn.send({ SetLanguage: languageSelect.value });
});

const ttlPresets: [number, string][] = [
  [3600, "1 hour"],
  [86400, "24 hours"],
  [7 * 86400, "7 days"],
  [30 * 86400, "30 days"],
  [365 * 86400, "1 year"],
  [100 * 365 * 86400, "100 years"],
];
for (const [secs, label] of ttlPresets) {
  const opt = document.createElement("option");
  opt.value = String(secs);
  opt.textContent = label;
  ttlSelect.appendChild(opt);
}
ttlSelect.value = "86400";
ttlSelect.disabled = readonly;
ttlSelect.addEventListener("change", () => {
  conn.send({ SetExpiry: { ttlSeconds: Number(ttlSelect.value) } });
});

copyLinkBtn.addEventListener("click", () => {
  navigator.clipboard.writeText(`${location.origin}/#${docId}`);
  copyLinkBtn.textContent = "Copied!";
  setTimeout(() => (copyLinkBtn.textContent = "Copy link"), 1200);
});

// The writable page can hand out a read-only share link.
if (!readonly) {
  const copyRoBtn = $<HTMLButtonElement>("#copy-readonly");
  fetch(`/api/readonlyid/${docId}`)
    .then((r) => (r.ok ? r.json() : Promise.reject(new Error(r.statusText))))
    .then(({ readonlyId }: { readonlyId: string }) => {
      copyRoBtn.hidden = false;
      copyRoBtn.addEventListener("click", () => {
        navigator.clipboard.writeText(`${location.origin}/#view/${readonlyId}`);
        copyRoBtn.textContent = "Copied!";
        setTimeout(() => (copyRoBtn.textContent = "Copy read-only link"), 1200);
      });
    })
    .catch(() => {});
}

renderMe();

// --- Connection -------------------------------------------------------------

function setStatus(online: boolean): void {
  statusEl.classList.toggle("online", online);
}

function showBanner(text: string): void {
  bannerEl.textContent = text;
  bannerEl.hidden = false;
}

const wsProto = location.protocol === "https:" ? "wss" : "ws";
const wsUrl = readonly
  ? `${wsProto}://${location.host}/api/readonly/${docId}`
  : `${wsProto}://${location.host}/api/socket/${docId}`;

const conn = new Connection(wsUrl, {
  onConnected() {
    if (killed) return;
    setStatus(true);
    sendMyInfo();
    client.resend();
    broadcastCursor();
  },
  onDisconnected(code) {
    setStatus(false);
    // The server re-sends full presence on reconnect; drop stale entries so
    // ghost users from dead connections don't accumulate.
    users.clear();
    cursors.clear();
    renderUsers();
    refreshRemoteCursors();
    if (code === 1008 && !killed) {
      // Policy violation: our OT state no longer matches the server (for
      // example, the server lost its history). Reconnecting would fail
      // forever; a reload resynchronizes from scratch.
      killed = true;
      showBanner("Out of sync with the server — reload the page to continue.");
      conn.dispose();
    }
  },
  onIdentity(id) {
    myId = id;
    client.me = id;
    renderUsers();
  },
  onHistory(start, ops) {
    client.handleHistory(start, ops);
  },
  onLanguage(lang) {
    if (languages[lang] === undefined) return;
    languageSelect.value = lang;
    view.dispatch({ effects: languageCompartment.reconfigure(languageExtension(lang)) });
  },
  onUserInfo(id, info) {
    if (info) users.set(id, info);
    else {
      users.delete(id);
      cursors.delete(id);
    }
    renderUsers();
    refreshRemoteCursors();
  },
  onUserCursor(id, data) {
    cursors.set(id, data);
    refreshRemoteCursors();
  },
  onExpiry(ttlSeconds, expiresAt) {
    if (![...ttlSelect.options].some((o) => o.value === String(ttlSeconds))) {
      const opt = document.createElement("option");
      opt.value = String(ttlSeconds);
      opt.textContent = `${ttlSeconds}s (custom)`;
      ttlSelect.appendChild(opt);
    }
    ttlSelect.value = String(ttlSeconds);
    expiresAtEl.textContent = `expires ${new Date(expiresAt * 1000).toLocaleString()}`;
  },
  onKilled(reason) {
    killed = true;
    showBanner(`This document is no longer available: ${reason}`);
    view.dispatch({
      effects: readOnlyCompartment.reconfigure([EditorState.readOnly.of(true), EditorView.editable.of(false)]),
    });
    conn.dispose();
    setStatus(false);
  },
});
