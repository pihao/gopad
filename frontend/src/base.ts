// Gopad can be mounted under a URL prefix (the server's BASE_PATH) so it can
// live at e.g. https://example.com/gopad/ behind a reverse proxy. The server
// injects a <base href> into the page it serves; resolving "." against it
// yields the mount point. Always ends with a slash, so build URLs by
// appending a relative path: `${basePath}api/socket/${id}`.
//
// Without the injected tag — the vite dev server serves the HTML sources
// verbatim — this falls back to the document's own directory, i.e. "/".
export const basePath = new URL(".", document.baseURI).pathname;
