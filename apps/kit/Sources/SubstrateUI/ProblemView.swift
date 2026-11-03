#if canImport(SwiftUI)
import SwiftUI
import SubstrateScreens

/// A problem and the one thing that fixes it, as its presentation words it.
public struct ProblemView: View {
    let problem: ProblemPresentation
    let fix: (ProblemFix.Action) -> Void

    public init(_ problem: ProblemPresentation, fix: @escaping (ProblemFix.Action) -> Void = { _ in }) {
        self.problem = problem
        self.fix = fix
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ProblemBox(problem)
            if let button = problem.fix {
                Button(button.title) { fix(button.action) }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
            }
        }
    }
}
#endif
