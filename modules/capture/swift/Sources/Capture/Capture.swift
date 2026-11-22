import Foundation
#if canImport(AppKit)
import AppKit
import ApplicationServices

// The implementation of package capture on macOS: the accessibility API
// first, and a simulated ⌘C or ⌘V through the clipboard where it reads or
// writes nothing, with the clipboard restored afterwards.

func makeCapture() throws -> any Capture { SystemCapture() }

final class SystemCapture: Capture {
    /// Where GetSelectedText read last, for ReplaceSelection.
    private struct Target {
        var pid: pid_t
        var element: AXUIElement?
        var wholeField: Bool
        var original: String
    }

    private let lock = NSLock()
    private var target: Target?

    // MARK: Capture

    func isTrusted() -> Bool { AXIsProcessTrusted() }

    func frontmostApp() throws -> AppInfo {
        guard let app = NSWorkspace.shared.frontmostApplication else { throw CaptureError.noTarget }
        return AppInfo(app)
    }

    func getSelectedText(opts: ReadOptions) throws -> Selection {
        guard AXIsProcessTrusted() else { throw CaptureError.notTrusted }
        guard let app = NSWorkspace.shared.frontmostApplication else { throw CaptureError.noTarget }
        var log: [String] = []
        let info = AppInfo(app)
        log.append("front app: \(info.name) (\(info.bundleID), pid \(info.pid))")
        let element = opts.ax || opts.wholeField ? focusedElement(pid: app.processIdentifier, log: &log) : nil

        func remember(_ text: String, whole: Bool) {
            lock.lock()
            target = Target(pid: app.processIdentifier, element: element, wholeField: whole, original: text)
            lock.unlock()
        }

        if opts.ax, let element {
            let selected = stringAttribute(element, kAXSelectedTextAttribute)
            log.append("AXSelectedText: \(describe(selected))")
            if let selected, !selected.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                remember(selected, whole: false)
                return Selection(text: selected, method: .methodAXSelection, app: info, wholeField: false,
                                 caret: caretRect(element), log: log)
            }
            if opts.wholeField, selected != nil, maySelectAll(element, bundleID: info.bundleID) {
                // An empty selection the app reports: the caret is in a
                // field that has nothing selected, so read all of it.
                let value = stringAttribute(element, kAXValueAttribute)
                log.append("AXValue: \(describe(value))")
                if let value, !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    remember(value, whole: true)
                    return Selection(text: value, method: .methodAXField, app: info, wholeField: true,
                                     caret: caretRect(element) ?? frameRect(element), log: log)
                }
            }
        } else if opts.ax {
            log.append("no focused element through the accessibility API")
        }

        if opts.clipboard {
            waitForModifiersReleased(log: &log)
            if let text = copyThroughClipboard(selectAll: false, log: &log) {
                remember(text, whole: false)
                return Selection(text: text, method: .methodClipboard, app: info, wholeField: false,
                                 caret: element.flatMap(caretRect), log: log)
            }
            if opts.wholeField, !maySelectAll(element, bundleID: info.bundleID) {
                log.append("⌘A skipped: the focus is not a text field")
            } else if opts.wholeField, let text = copyThroughClipboard(selectAll: true, log: &log) {
                // The selection ⌘A made may be gone by the time the user
                // replaces, so the paste selects all again first.
                remember(text, whole: true)
                return Selection(text: text, method: .methodClipboardAll, app: info, wholeField: true,
                                 caret: element.flatMap(frameRect), log: log)
            }
        }
        throw CaptureError.noText
    }

    func replaceSelection(text: String, opts: WriteOptions) throws -> WriteResult {
        guard AXIsProcessTrusted() else { throw CaptureError.notTrusted }
        lock.lock()
        let t = target
        lock.unlock()
        guard let t else { throw CaptureError.noTarget }
        var log: [String] = []

        if opts.ax, let element = t.element {
            let attribute = t.wholeField ? kAXValueAttribute : kAXSelectedTextAttribute
            let before = stringAttribute(element, kAXValueAttribute)
            if before == nil {
                // Without the value before and after, a write the app
                // reports done cannot be checked, and a paste after it
                // would insert the text twice: paste only.
                log.append("AXValue unreadable: not writing with the accessibility API")
            } else if !isSettable(element, attribute) {
                log.append("\(attribute) is not settable")
            } else {
                let err = AXUIElementSetAttributeValue(element, attribute as CFString, text as CFString)
                log.append("set \(attribute): \(axError(err))")
                if err == .success {
                    let after = stringAttribute(element, kAXValueAttribute)
                    if let after, after != before, after.contains(text) {
                        return WriteResult(method: .methodAX, verified: true, log: log)
                    }
                    log.append("AXValue unchanged after the write: \(describe(after))")
                }
            }
        }

        if opts.paste {
            try bringToFront(t.pid, log: &log)
            waitForModifiersReleased(log: &log)
            if t.wholeField {
                postKey(Key.a, flags: .maskCommand)
                usleep(60_000)
            }
            if pasteThroughClipboard(text, log: &log) {
                return WriteResult(method: .methodPaste, verified: false, log: log)
            }
        }
        throw CaptureError.writeFailed
    }

    func inspectFocus() throws -> Focus {
        guard AXIsProcessTrusted() else { throw CaptureError.notTrusted }
        guard let app = NSWorkspace.shared.frontmostApplication else { throw CaptureError.noTarget }
        var log: [String] = []
        guard let element = focusedElement(pid: app.processIdentifier, log: &log) else {
            return Focus(app: AppInfo(app), role: "", subrole: "", selectedText: "", value: "", valueLength: 0, log: log)
        }
        var names: CFArray?
        AXUIElementCopyAttributeNames(element, &names)
        let attributes = (names as? [String]) ?? []
        let settable = [kAXValueAttribute, kAXSelectedTextAttribute, kAXSelectedTextRangeAttribute]
            .filter { isSettable(element, $0) }
        let value = stringAttribute(element, kAXValueAttribute) ?? ""
        return Focus(
            app: AppInfo(app),
            role: stringAttribute(element, kAXRoleAttribute) ?? "",
            subrole: stringAttribute(element, kAXSubroleAttribute) ?? "",
            settable: settable,
            attributes: attributes,
            selectedText: String((stringAttribute(element, kAXSelectedTextAttribute) ?? "").prefix(200)),
            value: String(value.prefix(200)),
            valueLength: value.count,
            log: log
        )
    }

    // MARK: The accessibility API

    /// The focused element of the app, with Electron's and Chromium's
    /// accessibility tree turned on, which they build only for assistive
    /// apps that ask.
    private func focusedElement(pid: pid_t, log: inout [String]) -> AXUIElement? {
        let app = AXUIElementCreateApplication(pid)
        AXUIElementSetMessagingTimeout(app, 0.5)
        let manual = AXUIElementSetAttributeValue(app, "AXManualAccessibility" as CFString, kCFBooleanTrue)
        log.append("AXManualAccessibility: \(axError(manual))")
        for attempt in 0..<3 {
            var value: CFTypeRef?
            let err = AXUIElementCopyAttributeValue(app, kAXFocusedUIElementAttribute as CFString, &value)
            if err == .success, let value, CFGetTypeID(value) == AXUIElementGetTypeID() {
                let element = value as! AXUIElement
                AXUIElementSetMessagingTimeout(element, 0.5)
                log.append("focused element: \(stringAttribute(element, kAXRoleAttribute) ?? "?") (attempt \(attempt + 1))")
                return element
            }
            log.append("AXFocusedUIElement: \(axError(err)) (attempt \(attempt + 1))")
            // The tree turned on just now may take a moment to build.
            usleep(100_000)
        }
        return nil
    }

    /// Terminals, where ⌘A selects the whole scrollback rather than what
    /// the user types.
    private static let terminals: Set<String> = [
        "com.apple.Terminal", "com.googlecode.iterm2", "com.mitchellh.ghostty", "dev.warp.Warp-Stable",
        "net.kovidgoyal.kitty", "org.alacritty", "io.alacritty", "com.github.wez.wezterm", "co.zeit.hyper",
        "com.raphaelamorim.rio", "dev.commandline.waveterm",
    ]

    /// Whether the whole field, read with AXValue or ⌘A, is only what the
    /// user types: never in a terminal, whose field is its scrollback; else
    /// when the focus is a text input, or the accessibility API says nothing.
    private func maySelectAll(_ element: AXUIElement?, bundleID: String) -> Bool {
        if Self.terminals.contains(bundleID) { return false }
        guard let element, let role = stringAttribute(element, kAXRoleAttribute) else { return true }
        return [kAXTextAreaRole, kAXTextFieldRole, kAXComboBoxRole, "AXSearchField"].contains(role)
    }

    private func stringAttribute(_ element: AXUIElement, _ name: String) -> String? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success else { return nil }
        if let s = value as? String { return s }
        if let a = value as? NSAttributedString { return a.string }
        return nil
    }

    private func isSettable(_ element: AXUIElement, _ name: String) -> Bool {
        var settable = DarwinBoolean(false)
        return AXUIElementIsAttributeSettable(element, name as CFString, &settable) == .success && settable.boolValue
    }

    /// Where the selection is on the screen.
    private func caretRect(_ element: AXUIElement) -> Rect? {
        var range: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, kAXSelectedTextRangeAttribute as CFString, &range) == .success,
              let range else { return nil }
        var bounds: CFTypeRef?
        guard AXUIElementCopyParameterizedAttributeValue(
            element, kAXBoundsForRangeParameterizedAttribute as CFString, range, &bounds) == .success,
              let bounds, CFGetTypeID(bounds) == AXValueGetTypeID() else { return nil }
        var r = CGRect.zero
        guard AXValueGetValue(bounds as! AXValue, .cgRect, &r), r.width >= 0, r.height > 0 else { return nil }
        return Rect(x: r.origin.x, y: r.origin.y, width: r.width, height: r.height)
    }

    /// The element's frame on the screen.
    private func frameRect(_ element: AXUIElement) -> Rect? {
        var pos: CFTypeRef?
        var size: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, kAXPositionAttribute as CFString, &pos) == .success,
              AXUIElementCopyAttributeValue(element, kAXSizeAttribute as CFString, &size) == .success,
              let pos, let size else { return nil }
        var p = CGPoint.zero
        var s = CGSize.zero
        guard AXValueGetValue(pos as! AXValue, .cgPoint, &p), AXValueGetValue(size as! AXValue, .cgSize, &s) else { return nil }
        return Rect(x: p.x, y: p.y, width: s.width, height: s.height)
    }

    // MARK: The clipboard

    /// Copies the selection with ⌘C (after ⌘A with selectAll) and restores
    /// the clipboard. Nil when the app copied nothing.
    private func copyThroughClipboard(selectAll: Bool, log: inout [String]) -> String? {
        let pb = NSPasteboard.general
        let saved = ClipboardSnapshot(pb)
        let before = pb.changeCount
        if selectAll {
            postKey(Key.a, flags: .maskCommand)
            usleep(60_000)
        }
        postKey(Key.c, flags: .maskCommand)
        var copied: String?
        // Apps copy when they handle the key press, which an app that is
        // busy does later: wait for the clipboard to change.
        for _ in 0..<40 {
            usleep(15_000)
            if pb.changeCount != before {
                usleep(20_000) // let the app write every type
                copied = pb.string(forType: .string)
                break
            }
        }
        let name = selectAll ? "⌘A ⌘C" : "⌘C"
        if pb.changeCount == before {
            log.append("\(name): the clipboard did not change")
            return nil
        }
        saved.restore(pb)
        log.append("\(name): copied \(describe(copied)); clipboard restored")
        guard let copied, !copied.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
        return copied
    }

    /// Pastes text with ⌘V and restores the clipboard once the app has
    /// read it.
    private func pasteThroughClipboard(_ text: String, log: inout [String]) -> Bool {
        let pb = NSPasteboard.general
        let saved = ClipboardSnapshot(pb)
        pb.clearContents()
        let item = NSPasteboardItem()
        item.setString(text, forType: .string)
        // Clipboard managers leave out what is marked transient.
        item.setData(Data(), forType: NSPasteboard.PasteboardType("org.nspasteboard.TransientType"))
        item.setData(Data(), forType: NSPasteboard.PasteboardType("org.nspasteboard.AutoGeneratedType"))
        guard pb.writeObjects([item]) else {
            log.append("could not write the clipboard")
            return false
        }
        let ours = pb.changeCount
        postKey(Key.v, flags: .maskCommand)
        // Apps read the clipboard as they handle ⌘V, which may be late;
        // restoring too early would paste what it held before.
        usleep(450_000)
        if pb.changeCount == ours {
            saved.restore(pb)
            log.append("⌘V posted; clipboard restored")
        } else {
            log.append("⌘V posted; the clipboard changed since, left as it is")
        }
        return true
    }

    // MARK: Keys and apps

    private enum Key {
        static let a: CGKeyCode = 0x00
        static let c: CGKeyCode = 0x08
        static let v: CGKeyCode = 0x09
    }

    private func postKey(_ key: CGKeyCode, flags: CGEventFlags) {
        let source = CGEventSource(stateID: .privateState)
        for down in [true, false] {
            guard let e = CGEvent(keyboardEventSource: source, virtualKey: key, keyDown: down) else { continue }
            e.flags = flags
            e.post(tap: .cghidEventTap)
            usleep(8_000)
        }
    }

    /// Waits, up to half a second, until the user lets go of the keys of
    /// the shortcut, whose ⌥ or ⇧ would otherwise join the simulated
    /// ⌘C or ⌘V.
    private func waitForModifiersReleased(log: inout [String]) {
        let held: CGEventFlags = [.maskAlternate, .maskShift, .maskControl, .maskCommand]
        for i in 0..<50 {
            if CGEventSource.flagsState(.combinedSessionState).intersection(held).isEmpty {
                if i > 0 { log.append("waited \(i * 10) ms for the modifier keys") }
                return
            }
            usleep(10_000)
        }
        log.append("modifier keys still held after 500 ms")
    }

    /// Brings the app back to the front, as Kintsugi's panel may have
    /// taken the keyboard.
    private func bringToFront(_ pid: pid_t, log: inout [String]) throws {
        guard let app = NSRunningApplication(processIdentifier: pid), !app.isTerminated else {
            throw CaptureError.noTarget
        }
        if NSWorkspace.shared.frontmostApplication?.processIdentifier != pid {
            app.activate()
            log.append("activated \(app.localizedName ?? "the app")")
        }
        for _ in 0..<30 where NSWorkspace.shared.frontmostApplication?.processIdentifier != pid {
            usleep(10_000)
        }
        usleep(80_000) // let its window take the keyboard
    }

    // MARK: Logs

    private func describe(_ s: String?) -> String {
        guard let s else { return "unavailable" }
        if s.isEmpty { return "empty" }
        let flat = s.replacingOccurrences(of: "\n", with: "⏎")
        return "\(s.count) characters \"\(flat.prefix(40))\(flat.count > 40 ? "…" : "")\""
    }

    private func axError(_ e: AXError) -> String {
        switch e {
        case .success: return "ok"
        case .attributeUnsupported: return "attribute unsupported"
        case .noValue: return "no value"
        case .apiDisabled: return "API disabled"
        case .cannotComplete: return "cannot complete"
        case .notImplemented: return "not implemented"
        case .illegalArgument: return "illegal argument"
        case .invalidUIElement: return "invalid element"
        default: return "error \(e.rawValue)"
        }
    }
}

/// What the clipboard held, every item in every type, to put back.
private struct ClipboardSnapshot {
    let items: [[NSPasteboard.PasteboardType: Data]]

    init(_ pb: NSPasteboard) {
        items = (pb.pasteboardItems ?? []).map { item in
            var types: [NSPasteboard.PasteboardType: Data] = [:]
            for type in item.types {
                if let data = item.data(forType: type) { types[type] = data }
            }
            return types
        }
    }

    func restore(_ pb: NSPasteboard) {
        pb.clearContents()
        guard !items.isEmpty else { return }
        let restored = items.map { types -> NSPasteboardItem in
            let item = NSPasteboardItem()
            for (type, data) in types { item.setData(data, forType: type) }
            return item
        }
        pb.writeObjects(restored)
    }
}

private extension AppInfo {
    init(_ app: NSRunningApplication) {
        self.init(bundleID: app.bundleIdentifier ?? "", name: app.localizedName ?? "", pid: Int(app.processIdentifier))
    }
}

#else

func makeCapture() throws -> any Capture { throw NativeError.unsupported }

#endif
