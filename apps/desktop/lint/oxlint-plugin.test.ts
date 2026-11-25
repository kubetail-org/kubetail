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

import { readFileSync } from 'node:fs';

import JSON5 from 'json5';
import { RuleTester } from 'oxlint/plugins-dev';
import { describe, it } from 'vitest';

import plugin from './oxlint-plugin.js';

RuleTester.describe = describe;
RuleTester.it = it;

const tester = new RuleTester();

// The options `.oxlintrc.json` gives the rule, so a ban dropped from the config fails here.
const config = JSON5.parse(readFileSync('.oxlintrc.json', 'utf8'));
const [, ...restrictions] = config.rules['kubetail/no-restricted-syntax'];

const SINK = 'Cluster data is attacker-controlled text; render it as text, never as HTML.';
const EVAL = 'No eval in the webview.';
const TEMPLATE =
  'Cluster data is attacker-controlled text; read it with `src/lib/jsonpath.ts`, never a template engine.';

const restricted = (code: string, message: string) => ({
  code,
  filename: 'bad.tsx',
  options: restrictions,
  errors: [{ message }],
});

tester.run('no-restricted-syntax', plugin.rules['no-restricted-syntax'], {
  valid: [
    { code: 'el.textContent = html;', options: restrictions },
    { code: 'el.setAttribute("title", html);', options: restrictions },
    { code: 'const out = items.map((item) => item.name);', options: restrictions },
    { code: 'for (const v of xs) f(v);', options: restrictions },
    { code: 'const f = () => <iframe title="x" src="/page" />;', filename: 'ok.tsx', options: restrictions },
    { code: 'el.innerHTML = html;', options: [] },
  ],
  invalid: [
    restricted('for (const k in o) f(k);', 'Use Object.{keys,values,entries} and iterate.'),
    restricted('el.innerHTML = html;', SINK),
    restricted('el.outerHTML = html;', SINK),
    restricted('el.insertAdjacentHTML("beforeend", html);', SINK),
    restricted('range.createContextualFragment(html);', SINK),
    restricted('document.write(html);', SINK),
    restricted('document.writeln(html);', SINK),
    restricted('el["innerHTML"] = html;', SINK),
    restricted('const f = () => <iframe title="x" srcDoc={html} />;', 'No inline frames from data.'),
    restricted('const f = () => <iframe title="x" srcdoc={html} />;', 'No inline frames from data.'),
    restricted('eval(code);', EVAL),
    restricted('new Function(code);', EVAL),
    restricted('template(source)(data);', TEMPLATE),
    restricted('Handlebars.template(spec);', TEMPLATE),
  ],
});

const NEEDS_REASON = 'Say why the rule is wrong here: `-- <reason>` after the rule names.';

tester.run('disable-needs-reason', plugin.rules['disable-needs-reason'], {
  valid: [
    '// oxlint-disable-next-line no-bitwise -- the hash is bitwise by definition\nconst h = a | b;',
    '/* oxlint-disable no-bitwise -- a hash table */',
    '// eslint-disable-line no-bitwise -- a hash',
    '// oxlint-enable no-bitwise',
    '// a comment that mentions oxlint-disable in passing',
  ],
  invalid: [
    { code: '// oxlint-disable-next-line no-bitwise\nconst h = a | b;', errors: [{ message: NEEDS_REASON }] },
    { code: '/* oxlint-disable */', errors: [{ message: NEEDS_REASON }] },
    { code: '// eslint-disable-line no-bitwise', errors: [{ message: NEEDS_REASON }] },
    { code: '// oxlint-disable-next-line no-bitwise --', errors: [{ message: NEEDS_REASON }] },
    { code: '// oxlint-disable-next-line no-bitwise --   ', errors: [{ message: NEEDS_REASON }] },
  ],
});
