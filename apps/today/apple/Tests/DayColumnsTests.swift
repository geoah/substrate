#if os(macOS)
import CoreGraphics
import Testing

@testable import SubstrateTodayUI

/// How many columns the day's screen has in a window of a size
/// (`DayColumns`): the day alone, the day and the conversation, or the day,
/// the projects and the conversation.
@Suite struct DayColumnsTests {
    /// The windows the app is used in, in points, with the columns each has.
    @Test(arguments: [
        // An iPhone, upright; the largest on its side is too low for a conversation, and has a page beside the day as before.
        (CGSize(width: 402, height: 874), DayColumns.one),
        (CGSize(width: 956, height: 440), .beside),
        (CGSize(width: 874, height: 402), .one),
        // An iPhone Duo: closed, opened flat, and opened and turned upright.
        (CGSize(width: 466, height: 678), .one),
        (CGSize(width: 951, height: 669), .chat),
        (CGSize(width: 669, height: 951), .chat),
        // iPads upright: the mini, the 11-inch and the 13-inch.
        (CGSize(width: 744, height: 1133), .chat),
        (CGSize(width: 834, height: 1210), .chat),
        (CGSize(width: 1032, height: 1376), .chat),
        // iPads on their side.
        (CGSize(width: 1133, height: 744), .all),
        (CGSize(width: 1210, height: 834), .all),
        (CGSize(width: 1376, height: 1032), .all),
        // An iPad's window drawn narrow beside another app.
        (CGSize(width: 507, height: 1032), .one),
    ])
    func aWindowHasTheColumnsItHasRoomFor(size: CGSize, columns: DayColumns) {
        #expect(DayColumns.fit(width: size.width, height: size.height, chat: true) == columns)
    }

    /// With no conversation to show beside the day (a Mac, a drawing) the
    /// columns are what they were: the day, and a page beside it from 900
    /// points.
    @Test func withoutAConversationTheDayHasAPageBesideItOrNothing() {
        #expect(DayColumns.fit(width: 899, height: 800, chat: false) == .one)
        #expect(DayColumns.fit(width: 900, height: 800, chat: false) == .beside)
        #expect(DayColumns.fit(width: 1600, height: 1000, chat: false) == .beside)
        #expect(!DayColumns.beside.showsChat && !DayColumns.one.showsChat && DayColumns.chat.showsChat && DayColumns.all.showsChat)
    }

    /// The day is never the narrowest column, no column is narrower than a
    /// small iPhone, and the three fill the window between them.
    @Test(arguments: [660.0, 669, 744, 834, 951, 1032, 1099, 1100, 1133, 1210, 1376, 1800])
    func theColumnsShareTheWindowWithTheDayTheWidest(width: Double) {
        let width = CGFloat(width)
        let columns = DayColumns.fit(width: width, height: 900, chat: true)
        let chat = columns.chatWidth(in: width)
        let side = columns.sideWidth(in: width - chat)
        let day = width - chat - side
        #expect(chat >= 320 && day >= 340 && day >= chat)
        if columns == .all {
            #expect(side >= 320 && day >= side)
        } else {
            #expect(side == 0)
        }
    }
}
#endif
