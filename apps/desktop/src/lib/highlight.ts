// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Syntax highlighting for the code blocks in an answer, with every highlight.js
// grammar available and none in the bundle: each is its own chunk, fetched the
// first time a fence names it. What a grammar produces is a hast tree, rendered
// to React elements — never an HTML string.
import type { LanguageFn } from 'highlight.js';
import { createLowlight } from 'lowlight';

const low = createLowlight();

// One loader per grammar, keyed by its file name; Vite splits each into a chunk.
const loaders = import.meta.glob<{ default: LanguageFn }>('/node_modules/highlight.js/es/languages/*.js');

// A fence's word onto the grammar's file. Every grammar declares its own aliases,
// but only once loaded — this is the subset needed to find the file in the first
// place. A word not here and not a file name has no grammar.
const ALIASES: Record<string, string> = {
  sh: 'bash',
  zsh: 'bash',
  console: 'shell',
  shellsession: 'shell',
  yml: 'yaml',
  js: 'javascript',
  jsx: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  ts: 'typescript',
  tsx: 'typescript',
  py: 'python',
  golang: 'go',
  rs: 'rust',
  rb: 'ruby',
  kt: 'kotlin',
  cs: 'csharp',
  'c++': 'cpp',
  cc: 'cpp',
  h: 'c',
  hpp: 'cpp',
  html: 'xml',
  xhtml: 'xml',
  svg: 'xml',
  docker: 'dockerfile',
  toml: 'ini',
  jsonc: 'json',
  patch: 'diff',
  ps1: 'powershell',
  hcl: 'terraform',
  tf: 'terraform',
  md: 'markdown',
  proto: 'protobuf',
  plaintext: 'plaintext',
  text: 'plaintext',
  txt: 'plaintext',
};

function fileOf(lang: string): string | undefined {
  const name = ALIASES[lang.toLowerCase()] ?? lang.toLowerCase();
  return loaders[`/node_modules/highlight.js/es/languages/${name}.js`] ? name : undefined;
}

const pending = new Map<string, Promise<boolean>>();

/** Whether `lang`'s grammar is loaded, so `highlight` can run synchronously. */
export function loaded(lang: string): boolean {
  const name = fileOf(lang);
  return name !== undefined && low.registered(name);
}

/**
 * Loads `lang`'s grammar if there is one, resolving to whether there is. One load
 * per grammar however many blocks ask while it is in flight.
 */
export function load(lang: string): Promise<boolean> {
  const name = fileOf(lang);
  if (name === undefined) return Promise.resolve(false);
  if (low.registered(name)) return Promise.resolve(true);
  let p = pending.get(name);
  if (!p) {
    p = loaders[`/node_modules/highlight.js/es/languages/${name}.js`]()
      .then((mod) => {
        low.register(name, mod.default);
        return true;
      })
      .finally(() => pending.delete(name));
    pending.set(name, p);
  }
  return p;
}

/** The hast tree for `text` in `lang`. The grammar must be `loaded`. */
export function highlight(lang: string, text: string) {
  return low.highlight(fileOf(lang)!, text);
}
