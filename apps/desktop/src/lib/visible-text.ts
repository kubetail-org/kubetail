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

// Text the user must read before acting on it — the approval request's command,
// and the model's description beside it.
// A character that draws as nothing, or that reorders what follows it, is
// shown as its code point instead, so what the eye reads is what runs.

// One run of a text: its characters as they are, or one invisible character
// spelled out.
export type Segment = { text: string; spelled: boolean };

// A control character, a format character (bidirectional overrides, zero-width
// joiners and spaces, soft hyphens), a lone surrogate, a line or paragraph
// separator, any space that is not the ASCII one — a no-break space is one
// word to bash where the eye sees two — and every default-ignorable code
// point, which is where the invisibles of other categories live: the
// combining grapheme joiner (Mn), the Hangul fillers (Lo), the variation
// selectors. The Braille blank is none of these and draws as nothing. Tab and
// newline are kept: they read as what they are.
const INVISIBLE_CHAR = String.raw`(?![ \t\n])[\p{Cc}\p{Cf}\p{Cs}\p{Zl}\p{Zp}\p{Zs}\p{Default_Ignorable_Code_Point}\u{2800}]`;

// Each group makes split keep every match at an odd index.
const INVISIBLE = new RegExp(`(${INVISIBLE_CHAR})`, 'u');

// A run of spaces, tabs and invisibles that ends a line draws as nothing, and
// in a file it is still bytes: before a newline, or before the end of the text
// too, when the text runs to the end of the file.
const TRAILING = {
  lines: new RegExp(String.raw`((?:[ \t]|${INVISIBLE_CHAR})+)(?=\n)`, 'u'),
  end: new RegExp(String.raw`((?:[ \t]|${INVISIBLE_CHAR})+)(?=\n|$)`, 'u'),
};

// spell is the visible form of one invisible character.
export function spell(char: string): string {
  return `\\u{${char.codePointAt(0)!.toString(16).toUpperCase()}}`;
}

// visibleSegments splits text so every invisible character is its own spelled
// segment and everything between is kept verbatim. A caller draws the two
// kinds apart, since a command can also hold the six characters `\u{202E}`
// literally. With trailing, the spaces and tabs that end a line are spelled
// too: a file's content, where they land, rather than a command's, where the
// shell reads past them. With 'edges', as with 'end', and each newline that
// opens or closes the text is spelled too, since wrapped text shows neither: a
// variable's value, which a command reads whole.
export function visibleSegments(text: string, trailing?: 'lines' | 'end' | 'edges'): Segment[] {
  if (!trailing) return spellInvisible(text);
  if (trailing === 'edges') {
    const [, open, body, close] = /^(\n*)([\s\S]*?)(\n*)$/.exec(text)!;
    return [...spellAll(open), ...visibleSegments(body, 'end'), ...spellAll(close)];
  }
  return text.split(TRAILING[trailing]).flatMap((part, i) => (i % 2 ? spellAll(part) : spellInvisible(part)));
}

function spellAll(text: string): Segment[] {
  return [...text].map((char) => ({ text: spell(char), spelled: true }));
}

function spellInvisible(text: string): Segment[] {
  return text
    .split(INVISIBLE)
    .flatMap((part, i) =>
      part === '' ? [] : [i % 2 ? { text: spell(part), spelled: true } : { text: part, spelled: false }],
    );
}

// visibleTitle is text for a tooltip, which cannot mark a spelled character
// apart: each invisible character is its spelling alone.
export function visibleTitle(text: string): string {
  return spellInvisible(text)
    .map((segment) => segment.text)
    .join('');
}

// cutText splits text at whichever bound comes first: chars code points, or
// the newline that would start line lines+1. The rest is what a reader has to
// ask for, never what is dropped — a wall of newlines ahead of the command
// would otherwise push it out of sight, and a long command would be skimmed.
export function cutText(text: string, chars: number, lines: number): { head: string; rest: string } {
  let count = 0;
  let newlines = 0;
  let i = 0;
  while (i < text.length) {
    const cp = text.codePointAt(i)!;
    if (count === chars || (cp === 0x0a && newlines === lines - 1)) break;
    count += 1;
    if (cp === 0x0a) newlines += 1;
    i += cp > 0xffff ? 2 : 1;
  }
  return { head: text.slice(0, i), rest: text.slice(i) };
}

// descriptionLine is what is drawn of the model's description of a command: its
// first line with anything to say, cut to 200 characters. The rest stays in the
// call's arguments. `…` marks a cut only when what was cut holds more than
// whitespace, which is `\s` throughout.
export function descriptionLine(description: string): string {
  const { head, rest } = cutText(description.trimStart(), 200, 1);
  return /\S/.test(rest) ? `${head.trimEnd()}…` : head.trimEnd();
}
