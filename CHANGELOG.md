# Changelog

Each version's section is its release notes: the update window shows it,
and the release workflow puts it on the GitHub release.

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
