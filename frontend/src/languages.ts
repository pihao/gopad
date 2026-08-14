// The fixed set of selectable syntax-highlighting languages.
import type { Extension } from "@codemirror/state";
import { cpp } from "@codemirror/lang-cpp";
import { css } from "@codemirror/lang-css";
import { go } from "@codemirror/lang-go";
import { html } from "@codemirror/lang-html";
import { java } from "@codemirror/lang-java";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { markdown } from "@codemirror/lang-markdown";
import { python } from "@codemirror/lang-python";
import { rust } from "@codemirror/lang-rust";
import { sql } from "@codemirror/lang-sql";
import { xml } from "@codemirror/lang-xml";
import { yaml } from "@codemirror/lang-yaml";

export const languages: Record<string, () => Extension> = {
  plaintext: () => [],
  cpp: () => cpp(),
  css: () => css(),
  go: () => go(),
  html: () => html(),
  java: () => java(),
  javascript: () => javascript(),
  json: () => json(),
  markdown: () => markdown(),
  python: () => python(),
  rust: () => rust(),
  sql: () => sql(),
  typescript: () => javascript({ typescript: true }),
  xml: () => xml(),
  yaml: () => yaml(),
};

export function languageExtension(name: string): Extension {
  return (languages[name] ?? languages.plaintext)();
}
