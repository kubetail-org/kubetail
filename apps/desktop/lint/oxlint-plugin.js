// @ts-check
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

// Kubetail's own oxlint rules, loaded through `jsPlugins` in `.oxlintrc.json`. JavaScript, so Node
// loads it with no type stripping; `tsconfig.node.json` type-checks it.

import { definePlugin, defineRule } from '@oxlint/plugins';

/** @typedef {{ selector: string, message: string }} Restriction */

// ESLint's core rule of the same name, which oxlint lacks: each option is an AST selector and the
// message reported on a node it matches.
const noRestrictedSyntax = defineRule({
  meta: {
    docs: { description: 'Disallow the syntax each option names by AST selector.' },
    schema: {
      type: 'array',
      items: {
        type: 'object',
        properties: { selector: { type: 'string' }, message: { type: 'string' } },
        required: ['selector', 'message'],
        additionalProperties: false,
      },
    },
  },
  create(context) {
    /** @type {Record<string, (node: import('@oxlint/plugins').ESTree.Node) => void>} */
    const visitor = {};
    /** @type {Restriction[]} */ (context.options).forEach(({ selector, message }) => {
      visitor[selector] = (node) => context.report({ node, message });
    });
    return visitor;
  },
});

const DISABLE_DIRECTIVE = /^\s*(?:eslint|oxlint)-disable(?:-next-line|-line)?(?:\s|$)/;

// A suppression is a claim that the rule is wrong here; the reason after ` -- ` is what a reviewer
// checks the claim against.
const disableNeedsReason = defineRule({
  meta: {
    docs: { description: 'Require a `-- reason` on every lint-disable comment.' },
  },
  create(context) {
    return {
      Program() {
        context.sourceCode.getAllComments().forEach((comment) => {
          if (!DISABLE_DIRECTIVE.test(comment.value)) return;
          const reason = comment.value.split(/\s--\s/)[1]?.trim();
          if (reason) return;
          context.report({
            node: comment,
            message: 'Say why the rule is wrong here: `-- <reason>` after the rule names.',
          });
        });
      },
    };
  },
});

export default definePlugin({
  meta: { name: 'kubetail' },
  rules: {
    'disable-needs-reason': disableNeedsReason,
    'no-restricted-syntax': noRestrictedSyntax,
  },
});
