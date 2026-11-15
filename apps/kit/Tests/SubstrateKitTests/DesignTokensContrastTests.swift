import SubstrateScreens
import SubstrateUI
import Testing

/// The tokens that are words hold WCAG's 4.5:1 on every fill the kit and
/// the apps lay them on, in light and in dark: a value changed to one that
/// does not read fails here, by name.
@Suite("The design tokens' contrast", .timeLimit(.minutes(10)))
struct DesignTokensContrastTests {
    static let backgrounds: [(name: String, colour: DesignTokens.Colour)] = [("the page", DesignTokens.page), ("a grouped background", DesignTokens.grouped)]
    static let fills = [0.08, 0.1, 0.15, 0.16, DesignTokens.fillOpacity]

    @Test func theMathIsWCAGs() {
        #expect(DesignTokens.contrast(0x000000, 0xFFFFFF) == 21)
        #expect(DesignTokens.contrast(0xFFFFFF, 0xFFFFFF) == 1)
        // The pair every checker quotes: #767676 on white is 4.54:1.
        #expect(abs(DesignTokens.contrast(0x767676, 0xFFFFFF) - 4.54) < 0.01)
        #expect(DesignTokens.blend(0xFF0000, over: 0xFFFFFF, opacity: 0.5) == 0xFF8080)
        #expect(DesignTokens.blend(0x123456, over: 0xABCDEF, opacity: 1) == 0x123456)
        #expect(DesignTokens.blend(0x123456, over: 0xABCDEF, opacity: 0) == 0xABCDEF)
    }

    @Test func quietTextReadsOnEveryBackgroundAndPaleFill() {
        for background in Self.backgrounds {
            #expect(DesignTokens.quietText.contrast(on: background.colour) >= 4.5, "on \(background.name)")
            for tone in ScreenTone.allCases {
                for opacity in Self.fills {
                    let fill = DesignTokens.tone(tone).over(background.colour, opacity: opacity)
                    #expect(DesignTokens.quietText.contrast(on: fill) >= 4.5, "on \(tone) at \(opacity) over \(background.name)")
                }
            }
        }
    }

    @Test(arguments: ScreenTone.allCases)
    func aTonesWordsReadOnThePageAndOnItsOwnFill(_ tone: ScreenTone) {
        let text = DesignTokens.toneText(tone)
        for background in Self.backgrounds {
            #expect(text.contrast(on: background.colour) >= 4.5, "on \(background.name)")
            for opacity in Self.fills {
                let fill = DesignTokens.tone(tone).over(background.colour, opacity: opacity)
                #expect(text.contrast(on: fill) >= 4.5, "on its fill at \(opacity) over \(background.name)")
            }
        }
    }

    /// A mark is seen at 3:1, on the same fills its tone's words are read
    /// on.
    @Test(arguments: ScreenTone.allCases)
    func aTonesMarkIsSeenOnThePageAndOnItsOwnFill(_ tone: ScreenTone) {
        let mark = DesignTokens.toneMark(tone)
        for background in Self.backgrounds {
            #expect(mark.contrast(on: background.colour) >= 3, "on \(background.name)")
            for opacity in Self.fills {
                let fill = DesignTokens.tone(tone).over(background.colour, opacity: opacity)
                #expect(mark.contrast(on: fill) >= 3, "on its fill at \(opacity) over \(background.name)")
            }
        }
    }

    /// Why the tokens exist: the tones themselves, and the secondary text
    /// the kit's Android side draws, are not text colours on a white page.
    @Test func theTonesOwnColoursAreNotWords() {
        #expect(DesignTokens.contrast(DesignTokens.orange.light, DesignTokens.page.light) < 3)
        #expect(DesignTokens.contrast(DesignTokens.red.light, DesignTokens.page.light) < 4.5)
        #expect(DesignTokens.contrast(DesignTokens.secondary.light, DesignTokens.page.light) < 4.5)
        // Nor marks: a green check on a white page.
        #expect(DesignTokens.contrast(DesignTokens.green.light, DesignTokens.page.light) < 3)
    }
}

/// White words on a fill: every tone as a fill, and the fill an app with no
/// accent of its own draws a prominent button in.
@Suite("White on a fill")
struct DesignTokensFillTests {
    @Test(arguments: ScreenTone.allCases)
    func whiteReadsOnATonesFill(_ tone: ScreenTone) {
        #expect(DesignTokens.onFill.contrast(on: DesignTokens.toneFill(tone)) >= 4.5)
    }

    /// Why the fills exist: white on the system's own blue, orange and grey
    /// is under 4.5:1, which is what a prominent button and a swipe drew.
    @Test func whiteDoesNotReadOnTheTonesThemselves() {
        #expect(DesignTokens.contrast(0xFFFFFF, DesignTokens.blue.light) < 4.5)
        #expect(DesignTokens.contrast(0xFFFFFF, DesignTokens.orange.light) < 3)
        #expect(DesignTokens.contrast(0xFFFFFF, DesignTokens.gray.light) < 4.5)
    }

    /// The scale's steps as they have always been in points: a step is a
    /// whole number of the unit.
    @Test func theSpacingScaleIsItsUnitsMultiples() {
        #expect(DesignTokens.Spacing.unit == 2)
        #expect(DesignTokens.Spacing.allCases.map(\.value) == [2, 4, 6, 8, 10, 12, 14, 16, 20, 24, 28])
    }
}
