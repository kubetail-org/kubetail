// Copyright 2024-2025 Andres Morey
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

import { useState, useEffect } from 'react';
import { useTheme, Theme } from '@/lib/theme';

const LIGHT_THEME_COLORS = [
  '#d63031', // Strong Red
  '#00b894', // Strong Green
  '#0984e3', // Strong Blue
  '#fdcb6e', // Strong Yellow
  '#e17055', // Strong Orange
  '#6c5ce7', // Strong Purple
  '#00cec9', // Strong Cyan
  '#e84393', // Strong Magenta
  '#8b4513', // Strong Brown
  '#e91e63', // Strong Pink
  '#006064', // Strong Teal
  '#9c27b0', // Strong Lavender
  '#b71c1c', // Strong Maroon
  '#827717', // Strong Olive
  '#1a237e', // Strong Navy
  '#689f38', // Strong Lime
  '#ff8f00', // Strong Amber
  '#388e3c', // Strong Mint
  '#ff5722', // Strong Orange
  '#848484', // Grey
];

const DARK_THEME_COLORS = [
  '#ff5757', // Bright Red
  '#5cb85c', // Bright Green
  '#4da6ff', // Sky Blue
  '#ffeb3b', // Bright Yellow
  '#ff9800', // Bright Orange
  '#9c5cff', // Bright Purple
  '#26d0ce', // Bright Cyan
  '#ff4081', // Bright Pink
  '#d4a574', // Light Brown
  '#f06292', // Light Rose
  '#4db6ac', // Light Teal
  '#ba68c8', // Light Lavender
  '#ef5350', // Light Coral
  '#cddc39', // Light Olive
  '#7986cb', // Light Indigo
  '#8bc34a', // Fresh Lime
  '#ffc107', // Amber Gold
  '#81c784', // Light Mint
  '#ff7043', // Light Orange
  '#848484', // Light White
];

/**
 * SHA256 hash function that returns a number for color indexing
 */
const sha256Hash = async (str: string): Promise<number> => {
  const encoder = new TextEncoder();
  const data = encoder.encode(str);
  const hashBuffer = await crypto.subtle.digest('SHA-256', data);
  const hashArray = new Uint8Array(hashBuffer);

  // Convert first 4 bytes to a number for better distribution
  let hash = 0;
  for (let i = 0; i < 4; i += 1) {
    hash = (hash * 256) + hashArray[i];
  }

  return Math.abs(hash);
};

// Cache for storing computed color indices to avoid recalculating
const colorIndexCache = new Map<string, number>();
// Cache for in-progress hash calculations to prevent race conditions
const hashPromiseCache = new Map<string, Promise<number>>();

/**
 * Hook that returns a consistent color for a container
 * Uses SHA256 hashing for better distribution
 */
export const useContainerColor = (namespace: string, podName: string, containerName: string): string => {
  const { theme } = useTheme();
  const [color, setColor] = useState<string>('#848484'); // Default gray color

  useEffect(() => {
    const identifier = `${namespace}/${podName}/${containerName}`;

    // Check cache first
    const cachedIndex = colorIndexCache.get(identifier);

    if (cachedIndex !== undefined) {
      const colors = theme === Theme.Dark ? DARK_THEME_COLORS : LIGHT_THEME_COLORS;
      setColor(colors[cachedIndex]);
      return;
    }

    // Check if calculation is already in progress
    let hashPromise = hashPromiseCache.get(identifier);

    if (!hashPromise) {
      // Start new calculation and cache the promise
      hashPromise = sha256Hash(identifier);
      hashPromiseCache.set(identifier, hashPromise);
    }

    // Use the cached or new promise
    hashPromise.then((hash) => {
      const colors = theme === Theme.Dark ? DARK_THEME_COLORS : LIGHT_THEME_COLORS;
      const colorIndex = hash % colors.length;

      // Cache the computed index
      colorIndexCache.set(identifier, colorIndex);
      // Remove the promise from cache since it's complete
      hashPromiseCache.delete(identifier);

      setColor(colors[colorIndex]);
    });
  }, [namespace, podName, containerName, theme]);

  return color;
};

export { LIGHT_THEME_COLORS, DARK_THEME_COLORS };
