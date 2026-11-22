# Changelog

Each version's section is its release notes: the update window shows it,
and the release workflow puts it on the GitHub release.

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
