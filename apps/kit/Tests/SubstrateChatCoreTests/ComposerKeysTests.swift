import Testing

@testable import SubstrateChatCore

@Suite("Return in the composer", .timeLimit(.minutes(10)))
struct ReturnInTheComposer {
    @Test(arguments: ComposerKeys.Device.allCases)
    func commandReturnSendsOnEveryDevice(_ device: ComposerKeys.Device) {
        #expect(ComposerKeys.onReturn(.command, on: device) == .send)
    }

    @Test func onAMacReturnMakesANewLineTheComposerWrites() {
        #expect(ComposerKeys.onReturn([], on: .mac) == .newline)
        #expect(ComposerKeys.onReturn(.shift, on: .mac) == .newline)
        #expect(ComposerKeys.onReturn(.control, on: .mac) == .newline)
    }

    /// A Mac's field makes its own new line on Option-Return, with its own
    /// undo, so the composer leaves that press alone.
    @Test func onAMacOptionReturnIsTheFieldsOwnNewLine() {
        #expect(ComposerKeys.onReturn(.option, on: .mac) == .fieldsOwn)
        #expect(ComposerKeys.onReturn([.option, .shift], on: .mac) == .fieldsOwn)
    }

    /// An iPad's and an iPhone's field makes the new line itself, on the
    /// screen's keyboard and on a hardware one alike.
    @Test(arguments: [ComposerKeys.Device.pad, .phone], [ComposerKeys.Modifiers(), .shift, .option, .control, [.shift, .option]])
    func onAnIPadAndAnIPhoneReturnIsTheFieldsOwnNewLine(_ device: ComposerKeys.Device, _ modifiers: ComposerKeys.Modifiers) {
        #expect(ComposerKeys.onReturn(modifiers, on: device) == .fieldsOwn)
    }

    /// A send cannot be taken back, so only the chord that is named sends.
    @Test(arguments: ComposerKeys.Device.allCases, [ComposerKeys.Modifiers.shift, .option, .control])
    func commandWithAnotherKeyNeverSends(_ device: ComposerKeys.Device, _ other: ComposerKeys.Modifiers) {
        #expect(ComposerKeys.onReturn([.command, other], on: device) != .send)
    }

    /// The Return that confirms an input method's marked text is the input
    /// method's, whatever is held with it.
    @Test(arguments: ComposerKeys.Device.allCases, [ComposerKeys.Modifiers(), .command, .shift, .option])
    func aReturnThatConfirmsMarkedTextIsLeftToTheField(_ device: ComposerKeys.Device, _ modifiers: ComposerKeys.Modifiers) {
        #expect(ComposerKeys.onReturn(modifiers, on: device, composing: true) == .fieldsOwn)
    }

    @Test func aNewLineGoesWhereTheCursorIsAndTheCursorAfterIt() {
        #expect(ComposerKeys.newline(in: "") == ("\n", 1))
        #expect(ComposerKeys.newline(in: "one") == ("one\n", 4))
        #expect(ComposerKeys.newline(in: "onetwo", replacing: 3 ..< 3) == ("one\ntwo", 4))
        // Over a selection: it is replaced.
        #expect(ComposerKeys.newline(in: "one and two", replacing: 3 ..< 8) == ("one\ntwo", 4))
        // Characters of several scalars count as one each.
        #expect(ComposerKeys.newline(in: "👩🏽‍🚀é", replacing: 1 ..< 1) == ("👩🏽‍🚀\né", 2))
        // A selection past the draft's end is the end.
        #expect(ComposerKeys.newline(in: "one", replacing: 9 ..< 12) == ("one\n", 4))
    }

    /// A pasted draft may end in a carriage return, and a new line after
    /// one makes a single character of the two: the cursor is at the end
    /// of the draft, not one past it.
    @Test func aNewLineAfterACarriageReturnKeepsTheCursorInsideTheDraft() {
        let (text, cursor) = ComposerKeys.newline(in: "hello\r")
        #expect(text == "hello\r\n")
        #expect(text.count == 6)
        #expect(cursor == 6)
        let middle = ComposerKeys.newline(in: "a\rb", replacing: 2 ..< 2)
        #expect(middle.text == "a\r\nb")
        #expect(middle.cursor == 2)
        #expect(middle.cursor <= middle.text.count)
    }

    @Test func theSendButtonNamesTheShortcutWhereAHardwareKeyboardIsExpected() {
        for device in [ComposerKeys.Device.mac, .pad] {
            #expect(ComposerKeys.sendHelp(on: device) == "Send (⌘Return)")
            #expect(ComposerKeys.sendHint(on: device) == "Command-Return sends too.")
        }
        #expect(ComposerKeys.sendHelp(on: .phone) == nil)
        #expect(ComposerKeys.sendHint(on: .phone) == nil)
    }
}
