import path from 'path';

import { includeIgnoreFile } from '@eslint/compat';
import js from '@eslint/js';
import { configs, plugins } from 'eslint-config-airbnb-extended';
import { rules as prettierConfigRules } from 'eslint-config-prettier';
import prettierPlugin from 'eslint-plugin-prettier';
import reactRefresh from 'eslint-plugin-react-refresh';

export const projectRoot = path.resolve('.');
export const gitignorePath = path.resolve(projectRoot, '../.gitignore');

const jsConfig = [
  {
    name: 'js/config',
    ...js.configs.recommended,
  },
  plugins.stylistic,
  plugins.importX,
  ...configs.base.recommended,
];

const reactConfig = [plugins.react, plugins.reactHooks, plugins.reactA11y, ...configs.react.recommended];

const typescriptConfig = [plugins.typescriptEslint, ...configs.base.typescript, ...configs.react.typescript];

const prettierConfig = [
  // Prettier Plugin
  {
    name: 'prettier/plugin/config',
    plugins: {
      prettier: prettierPlugin,
    },
  },
  // Prettier Config
  {
    name: 'prettier/config',
    rules: {
      ...prettierConfigRules,
      'prettier/prettier': 'error',
    },
  },
];

const reactRefreshConfig = [reactRefresh.configs.recommended, reactRefresh.configs.vite];

const customRulesConfig = [
  {
    name: 'custom/rules',
    rules: {
      '@typescript-eslint/consistent-type-definitions': 'off',
      'consistent-return': 'off',
      'import-x/extensions': 'off',
      'import-x/no-extraneous-dependencies': 'off',
      'import-x/no-rename-default': 'off',
      'import-x/no-unresolved': 'off',
      'import-x/prefer-default-export': 'off',
      'max-classes-per-file': 'off',
      'no-console': 'off',
      'no-underscore-dangle': 'off',
      'prettier/prettier': ['error', { singleQuote: true, printWidth: 120 }],
      'react/function-component-definition': 'off',
      'react/jsx-no-target-blank': 'off',
      'react/prop-types': 'off',
      'react/react-in-jsx-scope': 'off',
      'react/require-default-props': 'off',
      'react-hooks/exhaustive-deps': 'off', // TODO: re-examine
      'react-refresh/only-export-components': 'off', // TODO: re-examine
    },
  },
];

export default [
  // Ignore .gitignore files/folder in eslint
  includeIgnoreFile(gitignorePath),
  {
    ignores: ['dist', 'codegen.ts', '**/__generated__/*.ts'],
  },
  ...jsConfig,
  ...reactConfig,
  ...typescriptConfig,
  ...prettierConfig,
  ...reactRefreshConfig,
  ...customRulesConfig,
];
