/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    './src/**/*.{ts,tsx}',
    './node_modules/@kubetail/ui/**/*.js',
	],
  plugins: [
    require('@kubetail/ui/plugin'),
    require('fancy-ansi/plugin')
  ],
  theme: {
    extend: {
      keyframes: {
        'flash-bg-green': {
          '0%': { backgroundColor: '#bbf7d0' }, // green
          '100%': { backgroundColor: 'transparent' }, // transparent
        }
      },
      animation: {
        'flash-bg-green': 'flash-bg-green 1s ease-in-out',
      }
    }
  }
}
