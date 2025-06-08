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

import { useEffect, useState } from 'react';

export enum Shape {
  CIRCLE = 'circle',
  SQUARE = 'square', 
  DIAMOND = 'diamond',
  TRIANGLE = 'triangle',
}

const AVAILABLE_SHAPES = [Shape.CIRCLE, Shape.SQUARE, Shape.DIAMOND, Shape.TRIANGLE];

// Storage key for container order tracking
const CONTAINER_ORDER_STORAGE_KEY = 'kubetail-container-order';

// Improved color palettes - 20 colors each theme
// Light theme: Darker, more saturated colors that show well on white backgrounds
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
  '#ff5722', // Strong Deep Orange
  '#424242', // Strong Grey
];

// Dark theme: Brighter colors that show well on dark backgrounds
const DARK_THEME_COLORS = [
  '#ff6b6b', // Bright Red
  '#51cf66', // Bright Green
  '#74c0fc', // Sky Blue
  '#ffd43b', // Bright Yellow
  '#ff922b', // Bright Orange
  '#cc5de8', // Bright Purple
  '#22d3ee', // Bright Cyan
  '#f783ac', // Bright Magenta
  '#d2691e', // Light Brown
  '#fbb6ce', // Light Pink
  '#20c997', // Light Teal
  '#d0bfff', // Light Lavender
  '#fa8072', // Light Coral
  '#a4ac86', // Light Olive
  '#4dabf7', // Light Navy
  '#94d82d', // Bright Lime
  '#f8f9c7', // Light Beige
  '#96f2d7', // Light Mint
  '#ffdecc', // Light Apricot
  '#adb5bd', // Light Grey
];

// Get or initialize container order from localStorage
const getContainerOrder = (): string[] => {
  try {
    const stored = localStorage.getItem(CONTAINER_ORDER_STORAGE_KEY);
    return stored ? JSON.parse(stored) : [];
  } catch {
    return [];
  }
};

// Add container to order list if not already present
const addContainerToOrder = (containerKey: string): number => {
  try {
    const order = getContainerOrder();
    const existingIndex = order.indexOf(containerKey);
    
    if (existingIndex !== -1) {
      return existingIndex;
    }
    
    // Add new container to the end
    order.push(containerKey);
    localStorage.setItem(CONTAINER_ORDER_STORAGE_KEY, JSON.stringify(order));
    return order.length - 1;
  } catch {
    // Fallback to hash-based index if localStorage fails
    return Math.abs(containerKey.split('').reduce((acc, char) => acc + char.charCodeAt(0), 0)) % 1000;
  }
};

export type ContainerShapeProps = {
  containerKey: string;
  size?: 'small' | 'medium';
  className?: string;
  theme?: 'light' | 'dark';
};

export const ContainerShape = ({ containerKey, size = 'medium', className = '', theme = 'light' }: ContainerShapeProps) => {
  // Use order-based assignment for shapes and hash-based for colors
  const getAssignment = async (key: string) => {
    // Get the order index (0-based) for this container
    const orderIndex = addContainerToOrder(key);
    
    // Use hash for consistent color assignment
    const streamUTF8 = new TextEncoder().encode(key);
    const buffer = await crypto.subtle.digest('SHA-256', streamUTF8);
    const view = new DataView(buffer);
    const colorSeed = view.getUint32(0);
    
    let shapeIndex: number;
    let colorIndex: number;
    
    if (orderIndex < 20) {
      // First 20 containers in order: All circles with sequential colors
      shapeIndex = 0; // Circle
      colorIndex = orderIndex % 20;
    } else if (orderIndex < 80) {
      // Next 60 containers: Random shapes (excluding circle) with random colors
      const nonCircleShapes = [1, 2, 3]; // Square, Diamond, Triangle
      const shapeSeed = view.getUint32(4);
      shapeIndex = nonCircleShapes[shapeSeed % nonCircleShapes.length];
      colorIndex = colorSeed % 20;
    } else {
      // 80+ containers: Random shapes (all 4) with random colors
      const shapeSeed = view.getUint32(4);
      shapeIndex = shapeSeed % AVAILABLE_SHAPES.length;
      colorIndex = colorSeed % 20;
    }
    
    return { shapeIndex, colorIndex };
  };

  const [shape, setShape] = useState<Shape>(Shape.CIRCLE);
  const [color, setColor] = useState<string>('#000000');

  useEffect(() => {
    const colors = theme === 'dark' ? DARK_THEME_COLORS : LIGHT_THEME_COLORS;
    
    getAssignment(containerKey).then(({ shapeIndex, colorIndex }) => {
      setShape(AVAILABLE_SHAPES[shapeIndex]);
      setColor(colors[colorIndex]);
    });
  }, [containerKey, theme]);

  const sizeClasses = size === 'small' ? 'w-[8px] h-[8px]' : 'w-[13px] h-[13px]';
  
  const baseStyle = {
    backgroundColor: color,
    display: 'inline-block',
  };

  switch (shape) {
    case Shape.CIRCLE:
      return (
        <div
          className={`${sizeClasses} rounded-full ${className}`}
          style={baseStyle}
        />
      );
    case Shape.SQUARE:
      return (
        <div
          className={`${sizeClasses} ${className}`}
          style={baseStyle}
        />
      );
    case Shape.DIAMOND:
      return (
        <div
          className={`${sizeClasses} ${className}`}
          style={{
            ...baseStyle,
            transform: 'rotate(45deg)',
          }}
        />
      );
    case Shape.TRIANGLE:
      return (
        <div
          className={`${sizeClasses} ${className}`}
          style={{
            ...baseStyle,
            width: 0,
            height: 0,
            backgroundColor: 'transparent',
            borderLeft: size === 'small' ? '4px solid transparent' : '6.5px solid transparent',
            borderRight: size === 'small' ? '4px solid transparent' : '6.5px solid transparent',
            borderBottom: size === 'small' ? `8px solid ${color}` : `13px solid ${color}`,
          }}
        />
      );
    default:
      return (
        <div
          className={`${sizeClasses} rounded-full ${className}`}
          style={baseStyle}
        />
      );
  }
};

// Export color arrays for use in other components if needed
export { LIGHT_THEME_COLORS, DARK_THEME_COLORS };

// Export utility function to clear container order (for testing/debugging)
export const clearContainerOrder = (): void => {
  try {
    localStorage.removeItem(CONTAINER_ORDER_STORAGE_KEY);
  } catch {
    // Ignore errors
  }
}; 