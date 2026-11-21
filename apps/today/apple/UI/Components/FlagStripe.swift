import SwiftUI
import TodayCore

extension Flag {
    /// The flag's colour. The three differ in lightness as well as hue, so
    /// they still read without colour.
    var color: Color {
        switch self {
        case .high: TodayLook.flagHigh.color
        case .medium: TodayLook.flagMedium.color
        case .low: TodayLook.flagLow.color
        }
    }
}

/// The only mark of a flag: one stripe, 4 points wide, filled for a flag
/// the owner set and hollow for one the assistant suggests. No flag draws
/// nothing. Urgent is never a stripe.
struct StripeView: View {
    var stripe: Stripe

    var body: some View {
        if let flag = stripe.flag {
            let shape = Capsule()
            Group {
                if stripe.suggested {
                    shape.strokeBorder(flag.color, lineWidth: 1.25).frame(width: TodayLook.stripeWidth + 1)
                } else {
                    shape.fill(flag.color).frame(width: TodayLook.stripeWidth)
                }
            }
            .accessibilityHidden(true)
        }
    }
}

/// The swatch beside the word where a flag is chosen: the Flag line of an
/// open task and the option list. No flag is an empty outline there, so
/// the choice "None" still has a mark.
struct FlagSwatch: View {
    var stripe: Stripe

    var body: some View {
        Group {
            if stripe.flag == nil {
                Capsule().strokeBorder(TodayLook.ink.color.opacity(0.24), lineWidth: 1).frame(width: 5)
            } else {
                StripeView(stripe: stripe)
            }
        }
        .frame(width: 5, height: 20)
        .accessibilityHidden(true)
    }
}

extension View {
    /// The flag's stripe down this row's leading edge, inset from its top
    /// and bottom, with the row moved in to make room for it. No flag
    /// keeps no room.
    func stripe(_ stripe: Stripe, inset: CGFloat = 8, room: CGFloat = 10) -> some View {
        padding(.leading, stripe.flag == nil ? 0 : room)
            .overlay(alignment: .leading) {
                StripeView(stripe: stripe).padding(.vertical, inset)
            }
    }
}
