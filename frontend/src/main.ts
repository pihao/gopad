import "./style.css";
import { basicSetup } from "codemirror";
import { Annotation, Compartment, EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { oneDark } from "@codemirror/theme-one-dark";

import { generateName } from "./animals";
import { Connection } from "./connection";
import type { CursorData, UserInfo } from "./connection";
import { changesToOperation, cpToUtf16, operationToChanges, utf16ToCp } from "./conversion";
import { fmtDate, fmtRelative } from "./format";
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
const statusTextEl = $("#status-text");
const sidebarToggleEl = $<HTMLButtonElement>("#sidebar-toggle");
const userIconSVG =
  '<svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor" aria-hidden="true">' +
  '<path d="M8 8a3 3 0 1 0 0-6 3 3 0 0 0 0 6Zm0 1.5c-3.1 0-5.5 1.8-5.5 4V15h11v-1.5c0-2.2-2.4-4-5.5-4Z"/></svg>';
const bannerEl = $("#banner");
const usersEl = $<HTMLUListElement>("#users");
const nameInput = $<HTMLInputElement>("#name-input");
const hueBtn = $<HTMLButtonElement>("#hue-btn");
const languageSelect = $<HTMLSelectElement>("#language");
const ttlSelect = $<HTMLSelectElement>("#ttl");
const expiresAtEl = $("#expires-at");
const copyLinkBtn = $<HTMLButtonElement>("#copy-link");

$("#readonly-badge").hidden = !readonly;

// --- Sidebar toggle (drawer on mobile, collapsible column on desktop) -------

const mobileQuery = window.matchMedia("(max-width: 768px)");

function setSidebar(open: boolean, persist: boolean): void {
  document.body.classList.toggle("sidebar-open", open);
  sidebarToggleEl.textContent = open ? "❮" : "❯";
  // Only desktop toggles are remembered: the mobile drawer is transient,
  // and the computed initial state must not overwrite the preference.
  if (persist) localStorage.setItem("gopad-sidebar", open ? "1" : "0");
}

{
  const applyDefault = () => {
    const saved = localStorage.getItem("gopad-sidebar");
    setSidebar(mobileQuery.matches ? false : saved !== "0", false);
  };
  applyDefault();
  // Re-apply when crossing the mobile breakpoint (also covers environments
  // whose initial viewport size settles only after load).
  mobileQuery.addEventListener("change", applyDefault);
}

$("#sidebar-toggle").addEventListener("click", () => {
  setSidebar(!document.body.classList.contains("sidebar-open"), !mobileQuery.matches);
});
$("#backdrop").addEventListener("click", () => setSidebar(false, false));

// --- Local identity ---------------------------------------------------------

const storedName = localStorage.getItem("gopad-name");
const storedHue = Number(localStorage.getItem("gopad-hue"));
let myInfo: UserInfo = {
  name: storedName || generateName(),
  hue: Number.isFinite(storedHue) && storedHue >= 0 ? storedHue : Math.floor(Math.random() * 360),
};
nameInput.value = myInfo.name;

// --- Collaboration state ----------------------------------------------------

const users = new Map<number, UserInfo>();
const cursors = new Map<number, CursorData>();
// Bumped per cursor update; drives the caret label's show-then-fade replay.
const cursorStamps = new Map<number, number>();
let myId = -1;
let killed = false;
let expiresAt = 0; // unix seconds; 0 = unknown

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
      stamp: cursorStamps.get(id) ?? 0,
      cursors: data.cursors.map((c) => cpToUtf16(text, c)),
      selections: data.selections.map(([a, b]) => [cpToUtf16(text, a), cpToUtf16(text, b)] as [number, number]),
    });
  }
  view.dispatch({ effects: setRemoteCursors.of(sets) });
}

// --- Sidebar ----------------------------------------------------------------

function renderMe(): void {
  hueBtn.style.color = `hsl(${myInfo.hue}, 90%, 55%)`;
  nameInput.style.color = `hsl(${myInfo.hue}, 90%, 65%)`;
}

// The "me" row (#me-row) is a persistent element so re-rendering the list
// while the user is typing their name never steals focus; only the other
// users' rows are rebuilt.
function renderUsers(): void {
  for (const li of [...usersEl.children]) {
    if (li.id !== "me-row") li.remove();
  }
  const entries = [...users.entries()]
    .filter(([id]) => id !== myId)
    .sort((a, b) => a[0] - b[0]);
  for (const [, info] of entries) {
    const li = document.createElement("li");
    const icon = document.createElement("span");
    icon.className = "user-icon";
    icon.innerHTML = userIconSVG;
    const name = document.createElement("span");
    name.className = "user-name";
    name.textContent = info.name;
    name.style.color = `hsl(${info.hue}, 90%, 65%)`;
    li.append(icon, name);
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
  } else {
    nameInput.value = myInfo.name; // never commit an empty name
  }
});
nameInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") nameInput.blur();
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
        setTimeout(() => (copyRoBtn.textContent = "Copy read-only"), 1200);
      });
    })
    .catch(() => {});
}

renderMe();

// --- Connection -------------------------------------------------------------

// Connection states and wording follow rustpad's ConnectionStatus.
const statusTexts = {
  connected: "You are connected!",
  disconnected: "Connecting to the server...",
  desynchronized: "Disconnected, please refresh.",
} as const;

function setStatus(state: keyof typeof statusTexts): void {
  statusEl.className = `status-dot ${state}`;
  statusTextEl.textContent = statusTexts[state];
}

// The relative wording ("expires in 23 hours") goes stale, so re-render it
// every minute; the tooltip carries the absolute timestamp.
function renderExpiry(): void {
  if (!expiresAt) return;
  expiresAtEl.textContent = `expires ${fmtRelative(expiresAt)}`;
  expiresAtEl.title = fmtDate(expiresAt);
}
setInterval(renderExpiry, 60_000);

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
    setStatus("connected");
    sendMyInfo();
    client.resend();
    broadcastCursor();
  },
  onDisconnected(code) {
    setStatus("disconnected");
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
      setStatus("desynchronized");
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
      cursorStamps.delete(id);
    }
    renderUsers();
    refreshRemoteCursors();
  },
  onUserCursor(id, data) {
    cursors.set(id, data);
    cursorStamps.set(id, (cursorStamps.get(id) ?? 0) + 1);
    refreshRemoteCursors();
  },
  onExpiry(ttlSeconds, expiresAtSec) {
    if (![...ttlSelect.options].some((o) => o.value === String(ttlSeconds))) {
      const opt = document.createElement("option");
      opt.value = String(ttlSeconds);
      opt.textContent = `${ttlSeconds}s (custom)`;
      ttlSelect.appendChild(opt);
    }
    ttlSelect.value = String(ttlSeconds);
    expiresAt = expiresAtSec;
    renderExpiry();
  },
  onKilled(reason) {
    killed = true;
    showBanner(`This document is no longer available: ${reason}`);
    view.dispatch({
      effects: readOnlyCompartment.reconfigure([EditorState.readOnly.of(true), EditorView.editable.of(false)]),
    });
    conn.dispose();
    setStatus("desynchronized");
  },
});
