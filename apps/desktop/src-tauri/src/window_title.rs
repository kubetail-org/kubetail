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

//! A log window's native title, built from parts its page sends. The parts are
//! the model's, the user's and the cluster's text, so the OS gets them with
//! what `src/lib/visible-text.ts` treats as invisible stripped.

use unicode_properties::{GeneralCategory, UnicodeGeneralCategory};

/// Between the prefix and each part.
const SEPARATOR: &str = " · ";

/// The most characters of one part the title keeps.
const MAX_PART_CHARS: usize = 60;

/// Unicode 16's `Default_Ignorable_Code_Point` (`DerivedCoreProperties.txt`),
/// as inclusive ranges. It is where the invisibles of other categories live:
/// the combining grapheme joiner, the Hangul fillers, the variation selectors.
const DEFAULT_IGNORABLE: &[(char, char)] = &[
    ('\u{ad}', '\u{ad}'),
    ('\u{34f}', '\u{34f}'),
    ('\u{61c}', '\u{61c}'),
    ('\u{115f}', '\u{1160}'),
    ('\u{17b4}', '\u{17b5}'),
    ('\u{180b}', '\u{180f}'),
    ('\u{200b}', '\u{200f}'),
    ('\u{202a}', '\u{202e}'),
    ('\u{2060}', '\u{206f}'),
    ('\u{3164}', '\u{3164}'),
    ('\u{fe00}', '\u{fe0f}'),
    ('\u{feff}', '\u{feff}'),
    ('\u{ffa0}', '\u{ffa0}'),
    ('\u{fff0}', '\u{fff8}'),
    ('\u{1bca0}', '\u{1bca3}'),
    ('\u{1d173}', '\u{1d17a}'),
    ('\u{e0000}', '\u{e0fff}'),
];

/// Whether `c` draws as nothing. The ASCII space alone is kept: a title is one
/// line, so tab and newline go too.
fn invisible(c: char) -> bool {
    if c == ' ' {
        return false;
    }
    let class = matches!(
        c.general_category(),
        GeneralCategory::Control
            | GeneralCategory::Format
            | GeneralCategory::Surrogate
            | GeneralCategory::LineSeparator
            | GeneralCategory::ParagraphSeparator
            | GeneralCategory::SpaceSeparator
    );
    let ignorable = DEFAULT_IGNORABLE
        .iter()
        .any(|&(first, last)| (first..=last).contains(&c));
    class || ignorable || c == '\u{2800}'
}

/// The title for `parts`: `Logs`, then each part stripped and cut, joined by
/// [`SEPARATOR`].
pub fn build<S: AsRef<str>>(parts: &[S]) -> String {
    let mut title = String::from("Logs");
    for part in parts {
        title.push_str(SEPARATOR);
        title.push_str(&visible(part.as_ref(), MAX_PART_CHARS));
    }
    title
}

/// `part` with every character that draws as nothing dropped, cut to its first
/// `max_chars`: one line the OS shows as it is.
pub fn visible(part: &str, max_chars: usize) -> String {
    part.chars()
        .filter(|&c| !invisible(c))
        .take(max_chars)
        .collect()
}

#[cfg(test)]
mod tests {
    use super::build;

    #[test]
    fn logs_comes_first_and_the_parts_join_with_the_separator() {
        assert_eq!(build(&["chat", "pod"]), "Logs · chat · pod");
        assert_eq!(build::<&str>(&[]), "Logs");
    }

    /// An empty part is kept, so each part keeps its position.
    #[test]
    fn an_empty_part_is_kept() {
        assert_eq!(build(&["", "pod"]), "Logs ·  · pod");
    }

    /// One of each class `visible-text.ts` spells, but `Cs`, which a `&str`
    /// cannot hold.
    #[test]
    fn invisible_characters_are_dropped() {
        for (class, invisible) in [
            ("Cc", '\u{1b}'),
            ("Cc tab", '\t'),
            ("Cc newline", '\n'),
            ("Cf", '\u{202e}'),
            ("Zl", '\u{2028}'),
            ("Zp", '\u{2029}'),
            ("Zs", '\u{a0}'),
            ("Default_Ignorable (Mn)", '\u{34f}'),
            ("Default_Ignorable (Lo)", '\u{3164}'),
            ("Default_Ignorable (variation selector)", '\u{fe0f}'),
            ("Default_Ignorable (unassigned)", '\u{e0fff}'),
            ("Braille blank", '\u{2800}'),
        ] {
            assert_eq!(build(&[&format!("a{invisible}b")]), "Logs · ab", "{class}");
        }
    }

    #[test]
    fn the_space_is_kept() {
        assert_eq!(build(&["a b"]), "Logs · a b");
    }

    /// A part is cut to 60 characters, never mid-character.
    #[test]
    fn a_long_part_is_cut_on_a_character_boundary() {
        let part = "é".repeat(61);
        assert_eq!(build(&[&part]), format!("Logs · {}", "é".repeat(60)));
    }
}
