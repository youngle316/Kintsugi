// Package capture reads the text the user selected in another app, writes
// text back in its place. It is a native
// module implemented in Swift (swift/), with macOS's accessibility API and,
// where that reads nothing, a simulated ⌘C or ⌘V through the clipboard,
// which it restores afterwards.
//
// Reading and writing need the Accessibility permission: the
// accessibility API reads other apps only for trusted apps, and macOS
// drops the key presses of untrusted ones.
package capture

import "github.com/egoist/mygo/native"

// Method says how text was read or written.
type Method string

const (
	// MethodNone: nothing was read or written.
	MethodNone Method = "none"
	// MethodAXSelection read the selection with the accessibility API
	// (AXSelectedText).
	MethodAXSelection Method = "ax_selection"
	// MethodAXField read the whole focused field with the accessibility API
	// (AXValue), as nothing was selected.
	MethodAXField Method = "ax_field"
	// MethodClipboard read the selection with a simulated ⌘C.
	MethodClipboard Method = "clipboard"
	// MethodClipboardAll read the whole field with a simulated ⌘A, then ⌘C,
	// as nothing was selected.
	MethodClipboardAll Method = "clipboard_all"
	// MethodAX wrote with the accessibility API.
	MethodAX Method = "ax"
	// MethodPaste wrote with a simulated ⌘V.
	MethodPaste Method = "paste"
)

// AppInfo is a running app.
type AppInfo struct {
	BundleID string
	Name     string
	PID      int
}

// Rect is a rectangle on the screen in points, from the top left corner of
// the main display, as the accessibility API measures.
type Rect struct {
	X, Y, Width, Height float64
}

// ReadOptions choose how GetSelectedText may read.
type ReadOptions struct {
	// AX tries the accessibility API first.
	AX bool
	// Clipboard simulates ⌘C when the accessibility API read nothing.
	Clipboard bool
	// WholeField reads the whole focused field when nothing is selected:
	// its AXValue, or with Clipboard a simulated ⌘A before ⌘C.
	WholeField bool
}

// Selection is text read from the app in front.
type Selection struct {
	Text   string
	Method Method
	App    AppInfo
	// WholeField is true when Text is the whole field, read because
	// nothing was selected; ReplaceSelection then replaces all of it.
	WholeField bool
	// Caret is where the selection is on the screen, when the app says.
	Caret *Rect
	// Log says what each step found, for diagnostics.
	Log []string
}

// WriteOptions choose how ReplaceSelection may write.
type WriteOptions struct {
	// AX sets the text with the accessibility API first, and checks that
	// the field changed.
	AX bool
	// Paste simulates ⌘V (after ⌘A for a whole field) when the
	// accessibility API could not write, or was not tried.
	Paste bool
}

// WriteResult says how ReplaceSelection wrote.
type WriteResult struct {
	Method Method
	// Verified is true when the accessibility API read the field back
	// changed; a paste cannot be checked.
	Verified bool
	Log      []string
}

// Clip is what the clipboard holds.
type Clip struct {
	Text string
	// App is the app in front, most likely the one that copied.
	App AppInfo
	// Skip is true, with no Text, for what must not be kept: marked
	// concealed, transient or generated, or left by this module itself.
	Skip bool
}

var (
	// ErrNotTrusted: the app lacks the Accessibility permission.
	ErrNotTrusted = native.NewError("not_trusted")
	// ErrNoText: nothing was selected, or no method could read it.
	ErrNoText = native.NewError("no_text")
	// ErrNoTarget: there is nothing to write into, as no app is in front.
	ErrNoTarget = native.NewError("no_target")
	// ErrWriteFailed: no method could write the text.
	ErrWriteFailed = native.NewError("write_failed")
)

// Capture reads and writes the text of other apps.
//
//mygo:native
type Capture interface {
	// IsTrusted reports whether the app has the Accessibility permission.
	IsTrusted() bool
	// GetSelectedText reads the selected text of the app in front: with the
	// accessibility API, then with a simulated ⌘C, as opts allow. Read
	// through the clipboard, it restores what the clipboard held before.
	// It remembers where it read, for ReplaceSelection.
	GetSelectedText(opts ReadOptions) (Selection, error)
	// ReplaceSelection writes text in place of what GetSelectedText read
	// last, bringing that app to the front again if needed: with the
	// accessibility API, then with a simulated ⌘V, as opts allow. A paste
	// restores the clipboard afterwards.
	ReplaceSelection(text string, opts WriteOptions) (WriteResult, error)
	// ReadClip reads the text the clipboard holds, for the history. While
	// this module uses the clipboard itself, it waits.
	ReadClip() (Clip, error)
	// FrontApp is the app in front, to paste into later.
	FrontApp() (AppInfo, error)
	// PasteText brings the app with the process ID to the front and pastes
	// text with a simulated ⌘V, then restores the clipboard. It leaves
	// alone what GetSelectedText remembered.
	PasteText(text string, pid int) (WriteResult, error)
}
