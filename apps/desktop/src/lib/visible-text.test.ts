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

import { describe, expect, it } from 'vitest';

import { cutText, descriptionLine, spell, visibleSegments, visibleTitle } from './visible-text';

// Every character under test is written as an escape: this file must not hold
// the characters it is about.
const WHALE = '\u{1F40B}';

describe('visibleSegments', () => {
  it('keeps ordinary text, tabs and newlines as one verbatim segment', () => {
    expect(visibleSegments('ls -la\t~/x\necho ok')).toEqual([{ text: 'ls -la\t~/x\necho ok', spelled: false }]);
    expect(visibleSegments('')).toEqual([]);
  });

  // Each shape of the Trojan Source family: the override that draws what
  // follows it backwards, the zero-width space that splits a word the eye
  // reads whole, a C0 and a C1 control, the no-break space bash reads as a
  // word character, a line separator, a lone surrogate, and the invisibles
  // whose category says otherwise -- the combining grapheme joiner, the
  // Hangul filler, a variation selector, the Braille blank.
  it('spells every invisible or reordering character out', () => {
    [
      ['\u{202E}', '\\u{202E}'],
      ['\u{200B}', '\\u{200B}'],
      ['\u{1B}', '\\u{1B}'],
      ['\u{D}', '\\u{D}'],
      ['\u{85}', '\\u{85}'],
      ['\u{A0}', '\\u{A0}'],
      ['\u{2028}', '\\u{2028}'],
      ['\u{D800}', '\\u{D800}'],
      ['\u{34F}', '\\u{34F}'],
      ['\u{3164}', '\\u{3164}'],
      ['\u{FE0F}', '\\u{FE0F}'],
      ['\u{2800}', '\\u{2800}'],
    ].forEach(([char, spelled]) => {
      expect(spell(char)).toBe(spelled);
      expect(visibleSegments(`rm${char}-rf`)).toEqual([
        { text: 'rm', spelled: false },
        { text: spelled, spelled: true },
        { text: '-rf', spelled: false },
      ]);
    });
  });

  it('spells each of a run separately and a leading or trailing one alone', () => {
    expect(visibleSegments('\u{200B}\u{200B}ls\u{202E}')).toEqual([
      { text: '\\u{200B}', spelled: true },
      { text: '\\u{200B}', spelled: true },
      { text: 'ls', spelled: false },
      { text: '\\u{202E}', spelled: true },
    ]);
  });

  // A value keeps its inner whitespace as it is; a newline at either edge and
  // what ends a line are spelled.
  it('spells the edges of a value', () => {
    expect(visibleSegments('\nssh -i k\n  rm x \t\n', 'edges')).toEqual([
      { text: '\\u{A}', spelled: true },
      { text: 'ssh -i k\n  rm x', spelled: false },
      { text: '\\u{20}', spelled: true },
      { text: '\\u{9}', spelled: true },
      { text: '\\u{A}', spelled: true },
    ]);
    expect(visibleSegments('a  b', 'edges')).toEqual([{ text: 'a  b', spelled: false }]);
    expect(visibleSegments('', 'edges')).toEqual([]);
  });

  // The command can hold the spelling's own characters; the segment kind is
  // what tells them apart, so the caller can draw them differently.
  it('leaves a literal spelling as ordinary text', () => {
    expect(visibleSegments("printf '\\u{202E}'")).toEqual([{ text: "printf '\\u{202E}'", spelled: false }]);
  });

  // A mark that draws on the letter before it is visible and stays.
  it('leaves a visible combining mark alone', () => {
    expect(visibleSegments('caf\u{E9} cafe\u{301}')).toEqual([{ text: 'caf\u{E9} cafe\u{301}', spelled: false }]);
  });

  it('keeps an astral character whole', () => {
    expect(visibleSegments(`echo ${WHALE}\u{200B}`)).toEqual([
      { text: `echo ${WHALE}`, spelled: false },
      { text: '\\u{200B}', spelled: true },
    ]);
  });

  // A file's bytes are what a write puts in place, so a space or tab that ends
  // a line draws nothing on screen and still lands: each one is spelled.
  it('spells the spaces and tabs that end a line, when asked', () => {
    expect(visibleSegments('a \nb', 'lines')).toEqual([
      { text: 'a', spelled: false },
      { text: '\\u{20}', spelled: true },
      { text: '\nb', spelled: false },
    ]);
    expect(visibleSegments('a b\t', 'lines'), 'the end is not a line end').toEqual([{ text: 'a b\t', spelled: false }]);
    expect(visibleSegments('a b\t', 'end')).toEqual([
      { text: 'a b', spelled: false },
      { text: '\\u{9}', spelled: true },
    ]);
    expect(visibleSegments('  ', 'end')).toEqual([
      { text: '\\u{20}', spelled: true },
      { text: '\\u{20}', spelled: true },
    ]);
    expect(visibleSegments('a \u{200B} \n', 'lines')).toEqual([
      { text: 'a', spelled: false },
      { text: '\\u{20}', spelled: true },
      { text: '\\u{200B}', spelled: true },
      { text: '\\u{20}', spelled: true },
      { text: '\n', spelled: false },
    ]);
    expect(visibleSegments('a \nb '), 'a command keeps them').toEqual([{ text: 'a \nb ', spelled: false }]);
  });
});

describe('visibleTitle', () => {
  // A tooltip cannot mark a spelled character apart, so it carries the spelling alone.
  it('spells every invisible character and keeps the rest', () => {
    expect(visibleTitle('https://x\u202eevil')).toBe(`https://x${spell('\u202e')}evil`);
    expect(visibleTitle('arn:aws:iam::1:user/dev')).toBe('arn:aws:iam::1:user/dev');
  });
});

describe('cutText', () => {
  it('returns text within both bounds whole', () => {
    expect(cutText('ls\necho', 10, 5)).toEqual({ head: 'ls\necho', rest: '' });
    expect(cutText('', 10, 5)).toEqual({ head: '', rest: '' });
  });

  it('cuts at the character bound, counting code points', () => {
    expect(cutText('abcdef', 3, 5)).toEqual({ head: 'abc', rest: 'def' });
    expect(cutText(WHALE.repeat(3), 2, 5)).toEqual({ head: WHALE.repeat(2), rest: WHALE });
  });

  // The newline that would begin one line too many starts the rest, so the
  // head holds exactly `lines` lines and the fold sits at a line's end.
  it('cuts at the line bound, at the newline that would start the next line', () => {
    expect(cutText('a\nb\nc\nd', 100, 2)).toEqual({ head: 'a\nb', rest: '\nc\nd' });
    expect(cutText('\n\n\nrm -rf ~', 100, 2)).toEqual({ head: '\n', rest: '\n\nrm -rf ~' });
  });

  it('takes whichever bound comes first', () => {
    expect(cutText('abc\ndef', 2, 10)).toEqual({ head: 'ab', rest: 'c\ndef' });
    expect(cutText('a\nbcdefg', 100, 1)).toEqual({ head: 'a', rest: '\nbcdefg' });
  });
});

describe('descriptionLine', () => {
  it('keeps one line of 200 characters and marks the cut', () => {
    expect(descriptionLine('List files\nthen delete them')).toBe('List files…');
    expect(descriptionLine('x'.repeat(300))).toBe(`${'x'.repeat(200)}…`);
  });

  it('draws a short line whole', () => {
    expect(descriptionLine('List files')).toBe('List files');
  });

  it('marks no cut when only whitespace was cut', () => {
    expect(descriptionLine('List files\n')).toBe('List files');
    expect(descriptionLine('List files  \n\u00A0\n')).toBe('List files');
  });

  it('skips leading whitespace, blank lines included', () => {
    expect(descriptionLine('\n  \nList files')).toBe('List files');
  });

  it('is empty for a description with nothing to draw', () => {
    expect(descriptionLine('')).toBe('');
    expect(descriptionLine(' \n\t\u00A0')).toBe('');
  });
});
