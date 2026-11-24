# Changelog

Each version's section is its release notes: the update window shows it,
and the release workflow puts it on the GitHub release.

## 0.6.1

- Check text you paste, for apps whose selection cannot be read, such as
  Claude Code in a terminal: press ⌃⌘V, paste, then ⌘↩ checks it. ⌘↩ again
  copies the corrected text.
- The clipboard history of 0.6.0 is gone: nothing you copy is recorded, and
  what it kept is erased when Kintsugi starts.

## 0.6.0

- Clipboard history: press ⌃⌘V in any app, then Enter or ⌘1 to ⌘9 pastes a
  text you copied before; type to search, ⌘⌫ deletes.
- Recording is on after updating. The last 200 texts are kept on this Mac,
  unencrypted, and never sent. What apps mark as concealed or temporary is
  left out. Pause it from the panel or the menu bar, or turn it off and
  clear the history in Settings.

## 0.5.0

- A simpler panel: its actions are icons at the top, with their names and
  shortcuts in tooltips (✓ replaces, ✕ ignores or closes), and the footer
  is gone.
- Mistakes show their level as an icon, and the counts of errors and
  suggestions as icons with a number.
- Dictionary entries mark synonyms, opposites, collocations and word family
  with an icon, and keep at most 3 of each.

## 0.4.0

- Press ⌥⌘E in any app to explain the English you selected in plain
  English, with no Chinese: a sentence or a paragraph comes back in simple
  words, with its hard words, idioms and phrasal verbs explained, each with
  an example. Copy copies the explanation.
- A single word or a short phrase gets a learner's dictionary entry: its
  part of speech and register, its common meanings with an example each,
  synonyms, opposites, collocations and word family.
- Explain Selection in the menu bar, and the shortcut in Settings.
- Every lookup is kept on this Mac, for a vocabulary book to come.

## 0.3.0

- A new icon: a bowl mended with gold, the same in the menu bar, the Dock
  and the windows.
- Pin the correction panel to keep it up while you go back to writing; drag
  it by any free part of it.
- Quit in the menu bar quits again.
- The filters of Mistakes scroll through all the types, and fit in one row.
- Settings keep their labels on one line in a narrow window.

## 0.2.0

- A redesigned main window and correction panel.
- Every app is checked the same way; per-app capture modes are gone.
- The Capture Lab is removed.

## 0.1.0

The first release.

- Press ⌥⌘G in any app to check the English you selected, or the whole field
  when nothing is selected. A panel shows the corrected text, each mistake
  highlighted with its type, level and an explanation in Chinese or English,
  and replaces the text in place (⌘↩), copies it, or ignores it (Esc).
- Reads and writes with the accessibility API, and falls back to ⌘C/⌘V with
  the clipboard restored afterwards. Each app can use automatic, clipboard
  or manual capture.
- Every check and mistake is kept on this Mac: an overview with the error
  rate, the mistakes by type and your top 10 patterns, and a list of
  mistakes to filter by type, level, app and time.
- Settings for the SiliconFlow API key (kept in the Keychain), the models,
  the explanation language, the shortcut and opening at login.
- The Capture Lab (⌃⌥⌘L) tests how each app can be read and written.
- Updates itself from GitHub Releases.
