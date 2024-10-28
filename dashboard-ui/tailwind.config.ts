import kubetailUIPlugin from '@kubetail/ui/plugin';
import fancyAnsiPlugin from 'fancy-ansi/plugin';

/** @type {import('tailwindcss').Config} */
export default {
  content: [
    './src/**/*.{ts,tsx}',
    './node_modules/@kubetail/ui/**/*.js',
  ],
  plugins: [
    kubetailUIPlugin,
    fancyAnsiPlugin,
  ],
  theme: {
    extend: {
      keyframes: {
        'flash-bg-green': {
          '0%': { backgroundColor: '#bbf7d0' }, // green
          '100%': { backgroundColor: 'transparent' }, // transparent
        },
      },
      animation: {
        'flash-bg-green': 'flash-bg-green 1s ease-in-out',
      },
    },
  },
};
