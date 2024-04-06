module.exports = {
  root: true,
  env: { browser: true, es2020: true },
  extends: [
    'eslint:recommended',
    'plugin:@typescript-eslint/recommended',
    'plugin:react-hooks/recommended',
    'airbnb',
    'airbnb-typescript'
  ],
  ignorePatterns: [
    'dist',
    '.eslintrc.cjs',
    'codegen.ts',
    'vite.config.ts',
    '**/__generated__/*.ts',
  ],
  parser: '@typescript-eslint/parser',
  parserOptions: {
    project: './tsconfig.json'
  },
  plugins: ['react-refresh'],
  rules: {
    '@typescript-eslint/indent': 'off',
    '@typescript-eslint/no-explicit-any': 'off',
    'consistent-return': 'off',
    'import/extensions': 'off',
    'import/no-extraneous-dependencies': 'off',
    'import/prefer-default-export': 'off',
    'jsx-a11y/anchor-is-valid': 'off',
    'max-classes-per-file': 'off',
    'max-len': 'off',
    'no-console': 'off',
    'no-continue': 'off',
    'no-prototype-builtins': 'off',
    'no-restricted-syntax': 'off',
    'no-underscore-dangle': 'off',
    'object-curly-newline': 'off',
    'prefer-destructuring': ['error', { array: false, object: true }],
    'react/function-component-definition': 'off',
    'react/jsx-no-target-blank': 'off',
    'react/jsx-props-no-spreading': 'off',
    'react/react-in-jsx-scope': 'off',
    'react/require-default-props': 'off',
    'react-hooks/exhaustive-deps': 'off',
    'react-refresh/only-export-components': 'off', // re-examine this later
  },
}
