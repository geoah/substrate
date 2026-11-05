import Testing
import WorkoutsCore

@Suite struct NotesMarkdownTests {
    @Test func plainTextIsUnchanged() {
        #expect(NotesMarkdown.inline("Keep the bar close.\nBrace first.") == "Keep the bar close.\nBrace first.")
    }

    @Test func inlineSpansPassThrough() {
        #expect(NotesMarkdown.inline("**Slow** eccentric, _3s_ `tempo`") == "**Slow** eccentric, _3s_ `tempo`")
    }

    @Test func headingsAreBold() {
        #expect(NotesMarkdown.inline("## Cues ##\nChest up") == "**Cues**\nChest up")
        #expect(NotesMarkdown.inline("#hashtag") == "#hashtag")
    }

    @Test func listsReadAsBulletsAndNumbers() {
        let text = "- one\n* two\n  + nested\n1) first\n2. second\n- [ ] todo\n- [x] done"
        #expect(NotesMarkdown.inline(text) == "• one\n• two\n  • nested\n1. first\n2. second\n☐ todo\n☑ done")
    }

    @Test func quotesRulesAndFencesLoseTheirMarkers() {
        let text = "> Stay tight\n\n---\n\n```\n3x5 @ 80%\n```"
        #expect(NotesMarkdown.inline(text) == "Stay tight\n\n`3x5 @ 80%`")
    }

    @Test func blankRunsCollapseAndEdgesTrim() {
        #expect(NotesMarkdown.inline("\n\none\n\n\n\ntwo\n\n") == "one\n\ntwo")
    }

    @Test func aNumberThatIsNotAListStays() {
        #expect(NotesMarkdown.inline("3.5 kg plates") == "3.5 kg plates")
    }
}
