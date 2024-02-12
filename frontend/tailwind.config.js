/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    './src/**/*.{ts,tsx}',
    './node_modules/kubetail-ui/esm/**/*.js',
	],
  plugins: [
    require('kubetail-ui/plugin'),
  ],
}
