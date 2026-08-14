// The admin console: a paginated document table with delete actions.
// Served behind HTTP Basic Auth; the browser handles the credential prompt.
import { fmtDate } from "./format";

interface AdminDoc {
  id: string;
  sizeBytes: number;
  language: string;
  connections: number;
  createdAt: number;
  updatedAt: number;
  expiresAt: number;
}

interface AdminList {
  total: number;
  page: number;
  size: number;
  documents: AdminDoc[];
}

const PAGE_SIZE = 20;
let page = 1;

// Resolve API calls against an origin without credentials: when the page was
// opened via a user:pass@host URL, relative fetch() URLs are rejected.
const apiBase = `${location.protocol}//${location.host}`;

const rowsEl = document.querySelector("#rows") as HTMLTableSectionElement;
const totalEl = document.querySelector("#total") as HTMLElement;
const pageLabel = document.querySelector("#page-label") as HTMLElement;
const errorEl = document.querySelector("#error") as HTMLElement;
const prevBtn = document.querySelector("#prev") as HTMLButtonElement;
const nextBtn = document.querySelector("#next") as HTMLButtonElement;

function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(2)} MB`;
}

async function load(): Promise<void> {
  errorEl.textContent = "";
  let data: AdminList;
  try {
    const resp = await fetch(`${apiBase}/api/admin/documents?page=${page}&size=${PAGE_SIZE}`);
    if (!resp.ok) throw new Error(`${resp.status} ${resp.statusText}`);
    data = await resp.json();
  } catch (err) {
    errorEl.textContent = `Failed to load documents: ${err}`;
    return;
  }
  totalEl.textContent = `· ${data.total} documents`;
  const pages = Math.max(1, Math.ceil(data.total / PAGE_SIZE));
  if (page > pages) {
    page = pages;
  }
  pageLabel.textContent = `page ${page} / ${pages}`;
  prevBtn.disabled = page <= 1;
  nextBtn.disabled = page >= pages;

  rowsEl.replaceChildren();
  for (const doc of data.documents) {
    const tr = document.createElement("tr");

    const idCell = document.createElement("td");
    idCell.dataset.label = "Document";
    const link = document.createElement("a");
    link.href = `/#${doc.id}`;
    link.target = "_blank";
    link.textContent = doc.id;
    idCell.appendChild(link);

    const cells: [string, string][] = [
      ["Size", fmtSize(doc.sizeBytes)],
      ["Language", doc.language || "plaintext"],
      ["Conns", String(doc.connections)],
      ["Updated", fmtDate(doc.updatedAt)],
      ["Expires", fmtDate(doc.expiresAt)],
    ];
    const cellEls = cells.map(([label, text]) => {
      const td = document.createElement("td");
      td.dataset.label = label;
      td.textContent = text;
      return td;
    });

    const actionCell = document.createElement("td");
    actionCell.className = "actions";
    const del = document.createElement("button");
    del.type = "button";
    del.className = "danger";
    del.textContent = "Delete";
    del.addEventListener("click", async () => {
      if (!confirm(`Delete document "${doc.id}"? Connected users will be kicked.`)) return;
      const resp = await fetch(`${apiBase}/api/admin/documents/${doc.id}`, { method: "DELETE" });
      if (!resp.ok && resp.status !== 404) {
        errorEl.textContent = `Delete failed: ${resp.status} ${resp.statusText}`;
        return;
      }
      load();
    });
    actionCell.appendChild(del);

    tr.append(idCell, ...cellEls, actionCell);
    rowsEl.appendChild(tr);
  }
}

prevBtn.addEventListener("click", () => {
  page = Math.max(1, page - 1);
  load();
});
nextBtn.addEventListener("click", () => {
  page += 1;
  load();
});

load();
setInterval(load, 15_000);
