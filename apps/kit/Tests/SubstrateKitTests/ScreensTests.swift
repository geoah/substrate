import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing

private let now = ScreenFixtures.now
private let enUS = Locale(identifier: "en_US")
private let utc = TimeZone(identifier: "UTC")!

@Suite("The Access screen")
struct AccessScreenTests {
    func banner(_ issues: [AppIssue], offers: Set<AccessAction.Offer> = Set(AccessAction.Offer.allCases),
                locale: Locale = enUS) -> AccessBanner?
    {
        AccessPresenter.banner(issues: issues, appName: "Substrate Sync", host: "ada.example.com", offers: offers, now: now,
                               locale: locale, timeZone: utc)
    }

    @Test func noIssueIsNoBanner() {
        #expect(banner([]) == nil)
    }

    @Test func anApprovalSaysHowManyChangesAndOffersReviewWhereTheAppDoes() throws {
        let one = try #require(banner([.needsApproval(changes: [Change(kind: .scope, words: "See your places")])]))
        #expect(one.tone == .informative)
        #expect(one.lines[0].title == "Substrate Sync needs your approval")
        #expect(one.lines[0].message == "One change, which you approve on ada.example.com:")
        #expect(one.lines[0].action == ActionButton("Review", .review))
        #expect(one.lines[0].changes.map(\.symbol) == [.allowed])
        let none = try #require(banner([.needsApproval(changes: [Change(kind: .scope, words: "x")])], offers: []))
        #expect(none.lines[0].action == nil)
    }

    @Test func aLineListsThreeChangesAndCountsTheRest() throws {
        let changes = (1 ... 5).map { Change(kind: .scope, words: "Scope \($0)") }
        let line = try #require(banner([.needsApproval(changes: changes)])).lines[0]
        #expect(line.changes.map(\.words) == ["Scope 1", "Scope 2", "Scope 3"])
        #expect(line.more == "and 2 more")
    }

    @Test func aBlockedApprovalOffersTheServersFixWhereItNamedOne() throws {
        let block = AuthorizationSetup.Block(reason: "quarantined", problem: "Quarantined.", fix: "/catalog/x")
        let line = try #require(banner([.needsApproval(changes: [Change(kind: .upgrade, words: "Update X", detail: "Quarantined.", block: block)])]))
        #expect(line.tone == .caution)
        #expect(line.lines[0].title == "Something on ada.example.com is in the way")
        #expect(line.lines[0].action == ActionButton("Fix It on ada.example.com", .openFix("/catalog/x")))
        #expect(line.lines[0].changes[0] == ChangeLine(id: "upgrade|false|Update X", symbol: .warning, tone: .caution, words: "Update X",
                                                       detail: "Quarantined.", detailStands: true))
    }

    @Test func theEndOfAccessIsADayInTheEnvironmentsLocale() throws {
        let soon = now.addingTimeInterval(3 * 86400)
        #expect(try #require(banner([.accessEndsSoon(soon)])).lines[0].title == "Access ends November 6")
        #expect(try #require(banner([.accessEndsSoon(soon)], locale: Locale(identifier: "en_GB"))).lines[0].title == "Access ends 6 November")
        let nextYear = now.addingTimeInterval(90 * 86400)
        #expect(try #require(banner([.accessEndsSoon(nextYear)])).lines[0].title == "Access ends February 1, 2027")
    }

    @Test func aServerTooOldSaysWhatItRunsAndWhatItCannot() throws {
        let line = try #require(banner([.serverTooOld(running: "v0.113.0", needed: "0.114.0", missing: ["give apps access of their own"])]))
            .lines[0]
        #expect(line.message == "Substrate Sync needs Substrate 0.114.0 or later; ada.example.com runs 0.113.0. "
            + "It can't yet give apps access of their own. Whoever runs it can update it.")
        #expect(line.action == nil)
    }

    @Test func accessEndedSaysWhyInOneSentence() {
        #expect(AccessPresenter.ended(appName: "Substrate Sync", host: "ada.example.com").message
            == "ada.example.com no longer accepts Substrate Sync: the time you approved it for is up, or it was revoked in Settings, under Apps.")
    }

    @Test func aReviewHeadsItselfAndListsItsSections() {
        let changes = [Change(kind: .scope, words: "See your places"), Change(kind: .scope, words: "Your contacts", leftOut: true),
                       Change(kind: .install, words: "Install Apple")]
        let review = ReviewPresenter.present(appName: "Substrate Sync", host: "ada.example.com", changes: changes,
                                             heading: ReviewHeading(changes: changes, firstSignIn: false), alreadyAllowed: false, setsUp: true)
        #expect(review.title == "Allow more access and update server setup")
        #expect(review.sections.map(\.title) == ["It asks to also", "You left these out last time", "It will set up on your server"])
        #expect(review.footer.contains("Setting up comes with the request; to refuse it, deny."))
        let renew = ReviewPresenter.present(appName: "Substrate Sync", host: "ada.example.com", changes: [], heading: .renew,
                                            alreadyAllowed: false, setsUp: false)
        #expect(renew.intro == "Nothing new: Substrate Sync asks to keep the access you approved, for another while.")
    }
}

@Suite("The Connect screen")
struct ConnectScreenTests {
    let probe = ScreenFixtures.all.compactMap { fixture -> ServerProbe.Result? in
        if case let .connect(presentation) = fixture.presentation, fixture.name == "connect-found" { return presentation.found }
        return nil
    }[0]

    func controller(passwordDoor: Bool = false, opensBrowser: Bool = false) -> ConnectController {
        ConnectController(manifest: ScreenFixtures.manifest, locator: ServerLocator(probe: ServerProbe(), resolver: TableResolver([:])),
                          hasPasswordDoor: passwordDoor, opensBrowser: opensBrowser, signIn: { _ in })
    }

    @Test func anAddressIsCheckedThenTheServerFoundOffersTheBrowser() async throws {
        let harness = ControllerHarness(controller(), performsEffects: false)
        var presented = await harness.presentation
        #expect(presented.button == .init(title: "Continue", busy: false, enabled: false))
        #expect(presented.footer?.note.hasPrefix("Your repository's name") == true)
        await harness.send(.edit("ada.example.com"))
        await harness.send(.proceed)
        presented = await harness.presentation
        #expect(presented.button == .init(title: "Checking…", busy: true, enabled: false))
        #expect(presented.field.disabled)
        #expect(harness.effects == [.locate("ada.example.com")])
        await harness.deliver(.located(probe))
        presented = await harness.presentation
        #expect(presented.door == .browser)
        #expect(presented.field.text == "ada.example.com")
        #expect(presented.button?.title == "Sign In with Your Substrate")
        #expect(presented.footer?.found == "Server found: ada.example.com")
        #expect(presented.footer?.repository == "Repository: ada.example.com")
    }

    @Test func writingTheCheckedNameBackIsNotANewAddressButTypingIs() async throws {
        let harness = ControllerHarness(controller(), performsEffects: false)
        await harness.send(.edit("ada.example.com"))
        await harness.send(.proceed)
        await harness.deliver(.located(probe))
        await harness.send(.edit("ada.example.com"))
        #expect(await harness.presentation.door == .browser)
        await harness.send(.edit("ada.example.co"))
        #expect(await harness.presentation.door == ConnectPresentation.Door.none)
    }

    @Test func somethingThatIsNoAddressIsRefusedWithoutALookup() async throws {
        let harness = ControllerHarness(controller(), performsEffects: false)
        await harness.send(.edit("not an address"))
        await harness.send(.proceed)
        #expect(await harness.presentation.problem == ProblemPresentation(title: "Couldn't reach that server",
                                                                          message: "That is not a server address or a repository name."))
        #expect(harness.effects.isEmpty)
    }

    @Test func aLookupThatDidNotAnswerOffersTryAgain() async throws {
        let harness = ControllerHarness(controller(), performsEffects: false)
        await harness.send(.edit("ada.example.com"))
        await harness.send(.proceed)
        await harness.deliver(.locateFailed(problem: "The lookup did not answer.", retryable: true))
        let presented = await harness.presentation
        #expect(presented.button?.title == "Try Again")
        #expect(presented.problem?.message == "The lookup did not answer.")
    }

    @Test func aSignInInTheBrowserOffersCancelAndAFailureSaysWhy() async throws {
        let harness = ControllerHarness(controller(opensBrowser: true), performsEffects: false)
        await harness.send(.edit("ada.example.com"))
        await harness.send(.proceed)
        await harness.deliver(.located(probe))
        await harness.send(.proceed)
        var presented = await harness.presentation
        #expect(presented.button?.title == "Waiting for Your Browser…")
        #expect(presented.cancel == "Cancel")
        #expect(presented.footer?.note.contains("open in your browser") == true)
        #expect(harness.effects.last == .signIn(probe))
        await harness.send(.cancelSignIn)
        presented = await harness.presentation
        #expect(presented.button?.title == "Continue in Your Browser")
        #expect(harness.effects.last == .cancelSignIn)
        await harness.send(.proceed)
        await harness.deliver(.signInFailed(problem: "The code was refused."))
        #expect(await harness.presentation.problem == ProblemPresentation(title: "Couldn't sign in", message: "The code was refused."))
    }

    @Test func aServerTheAppWouldFallBackForIsTooOldWithoutAPasswordDoor() async throws {
        var old = probe
        old.metadata = nil
        old.verdict = .tooOld(running: "v0.113.0", needed: nil, missing: ["give apps access of their own"])
        var manifest = ScreenFixtures.manifest
        manifest.passwordFallback = .withoutAppAuthorization
        let without = ConnectController(manifest: manifest, locator: ServerLocator(probe: ServerProbe(), resolver: TableResolver([:])),
                                        signIn: { _ in })
        let input = PresenterInput<ConnectController>(state: ConnectState(address: "ada.example.com", phase: .found(old)), now: now)
        let tooOld = without.present(input)
        #expect(tooOld.door == .tooOld)
        #expect(tooOld.tooOld?.summary == "Substrate Tasks needs a newer Substrate. ada.example.com runs 0.113.0.")
        #expect(tooOld.button == nil)
        let with = ConnectController(manifest: manifest, locator: ServerLocator(probe: ServerProbe(), resolver: TableResolver([:])),
                                     hasPasswordDoor: true, signIn: { _ in })
        let password = with.present(PresenterInput(state: ConnectState(address: "ada.example.com", phase: .found(old)), now: now))
        #expect(password.door == .password)
        #expect(password.button == nil)
        #expect(password.footer?.note == "This server can't give Substrate Tasks access of its own yet, so your password goes to it directly.")
    }

    @Test func theSignInRunsAsAnEffectAndItsCancellationSaysNothing() async throws {
        let cancelled = ConnectController(manifest: ScreenFixtures.manifest,
                                          locator: ServerLocator(probe: ServerProbe(), resolver: TableResolver([:])),
                                          signIn: { _ in throw SignInCancelled() })
        let harness = ControllerHarness(cancelled, performsEffects: true)
        await harness.send(.edit("ada.example.com"))
        await harness.runtime.deliver(.located(probe))
        await harness.send(.proceed)
        try await harness.until { await $0.runtime.counters.delivered == 1 }
        let presented = await harness.presentation
        #expect(presented.problem == nil)
        #expect(presented.button?.title == "Sign In with Your Substrate")
    }
}

@Suite("The Account and Status screens")
struct AccountStatusTests {
    @Test func anAppSessionListsItsAccessAndWhenItEnds() {
        let session = SessionFacts(server: URL(string: "https://ada.example.com")!, repository: "ada.example.com",
                                   scopes: ["read:*"], expiresAt: now)
        let account = AccountPresenter.present(session: session, manifest: nil, serverVersion: "v0.114.0", unrevoked: [],
                                               offersChangeAccess: true, locale: enUS, timeZone: utc)
        #expect(account.rows == [LabeledValue("Server", "ada.example.com"), LabeledValue("Repository", "ada.example.com"),
                                 LabeledValue("Server version", "0.114.0")])
        #expect(account.approvedTitle == "Approved access")
        #expect(account.until?.value.hasPrefix("Nov 3, 2026 at 9:00") == true)
        #expect(account.changeAccess == "Change Access…")
        #expect(account.footer == "Sign Out: signs out on this device and revokes its access on ada.example.com.")
    }

    @Test func aPasswordSessionReachesEverything() {
        let account = AccountPresenter.present(session: SessionFacts(server: URL(string: "https://ada.example.com")!, repository: "ada.example.com"),
                                               manifest: nil, serverVersion: nil, unrevoked: [], offersChangeAccess: true, locale: enUS,
                                               timeZone: utc)
        #expect(account.access == LabeledValue("Access", "Everything, with your password"))
        #expect(account.changeAccess == nil && account.until == nil)
        #expect(account.footer.contains("revokes its sign-in"))
    }

    @Test func theStatusIsWhatBlocksMostInOrder() {
        #expect(LinkStatus.derive(signedIn: false, package: nil, outbox: .idle, waiting: 0, known: 1) == .local)
        #expect(LinkStatus.derive(signedIn: true, package: .current(held: 1), outbox: .signedOut, waiting: 2, known: 1) == .signedOut)
        #expect(LinkStatus.derive(signedIn: true, package: .current(held: 1), outbox: .idle, waiting: 0, known: 1,
                                  issues: [.needsApproval(changes: [])]) == .needsApproval)
        #expect(LinkStatus.derive(signedIn: true, package: nil, outbox: .idle, waiting: 0, known: 1) == .preparing)
        #expect(LinkStatus.derive(signedIn: true, package: .behind(held: 1), outbox: .idle, waiting: 0, known: 2) == .packageBehind(held: 1, known: 2))
        #expect(LinkStatus.derive(signedIn: true, package: .current(held: 2), outbox: .offline("x"), waiting: 3, known: 2)
            == .offline(waiting: 3, reason: "x"))
        #expect(LinkStatus.derive(signedIn: true, package: .current(held: 2), outbox: .sent, waiting: 0, known: 2) == .ready(waiting: 0))
    }

    @Test func aRefusedWriteIsListedWithRetryAndDiscard() {
        let write = ParkedWrite(id: UUID(), method: .patch, kind: "substrate.reamde.dev/tasks/task", recordID: "t1", words: "no", conflict: false)
        let status = StatusPresenter.present(.refused(waiting: 1, reason: "no"), words: StatusWords(app: "Tasks", device: "this iPhone",
                                                                                                    package: "Tasks", kept: "tasks"),
                                             host: "ada.example.com", package: .current(held: 2), known: 2, sending: false, waiting: 1,
                                             parked: [write], kindLabels: ["substrate.reamde.dev/tasks/task": "Task"], locale: enUS,
                                             timeZone: utc)
        #expect(status.sentence == "Your substrate refused a change: no 1 change waits; retry or discard it below.")
        #expect(status.refused?.rows.map(\.title) == ["Task, changed"])
        #expect(status.refused?.rows.first?.retry == ActionButton("Retry", .retry(write.id)))
        #expect(status.actions.map(\.title) == ["Send Now"])
    }

    @Test func aSizeReadsInDecimalUnits() {
        #expect(SettingsPresenter.byteWords(0) == "0 bytes")
        #expect(SettingsPresenter.byteWords(12_300) == "12.3 KB")
        #expect(SettingsPresenter.byteWords(1_000_000) == "1 MB")
        #expect(SettingsPresenter.byteWords(345_600_000) == "346 MB")
    }

    @Test func aSettingsCommandMovesItsValue() {
        let values = SettingsValues().applying(.toggle(row: "reminders", on: true)).applying(.choose(row: "sort", option: "name"))
        #expect(values.toggle("reminders") && values.choice("sort", default: "due") == "name")
        #expect(values.applying(.perform(row: "clear")) == values)
    }

    @Test func aStorageClearIsOfferedOnlyForWhatCanBeReadAgain() {
        let waitingOnly = SettingsPresenter.storage([AreaUsage(area: .outbox, bytes: 10, files: 1)], budget: nil)
        #expect(waitingOnly.rows.last?.control == .action(destructive: true, enabled: false))
    }
}

@Suite("The link")
struct LinkControllerTests {
    let secret = "sub_app_never_shown_9f1e"
    let server = URL(string: "https://ada.example.com")!

    func link(_ storage: MemoryStorage, folder: RepositoryFolder? = nil) -> (LinkController, AppEnvironment) {
        let store = SessionStore(storage: storage)
        let controller = LinkController(manifest: ScreenFixtures.manifest, sessions: store, packages: ["substrate.reamde.dev/tasks": 2],
                                        words: StatusWords(app: "Substrate Tasks", device: "this iPhone", package: "Tasks", kept: "tasks"))
        var environment = AppEnvironment.testing()
        environment.devices.connectivity = ScriptedConnectivity()
        environment.folder = folder
        return (controller, environment)
    }

    @Test func itOpensOnTheStoredSessionAndItsSnapshotCarriesNoSecret() async throws {
        let storage = MemoryStorage()
        let session = Session(server: server, repository: "ada.example.com", secret: secret, clientID: "https://tasks.example.com/client.json",
                              scopes: ["read:*"], expiresAt: now.addingTimeInterval(86400))
        try SessionStore(storage: storage).save(session)
        let (controller, environment) = link(storage, folder: try RepositoryFolder(root: temporaryFolder()))
        let harness = ControllerHarness(controller, environment: environment)
        await harness.send(.open)
        try await harness.until { harness in
            let presented = await harness.presentation
            return presented.snapshot.online == true && presented.account != nil
        }
        let presented = await harness.presentation
        #expect(presented.snapshot.phase == .signedIn)
        #expect(presented.snapshot.repository == "ada.example.com")
        #expect(presented.account?.rows.first == LabeledValue("Server", "ada.example.com"))
        let state = await harness.state
        let encoded = String(decoding: try JSONEncoder().encode(presented), as: UTF8.self)
            + String(decoding: try JSONEncoder().encode(state), as: UTF8.self)
        #expect(!encoded.contains(secret))
    }

    @Test func signedOutItSaysSoAndOffersTheWayIn() async throws {
        let (controller, environment) = link(MemoryStorage())
        let harness = ControllerHarness(controller, environment: environment)
        await harness.send(.open)
        try await harness.until { await $0.presentation.snapshot.online == true }
        let presented = await harness.presentation
        #expect(presented.snapshot.phase == .signedOut)
        #expect(presented.account == nil)
        #expect(presented.status.sentence == "Not signed in. Tasks stay on this iPhone.")
        #expect(presented.status.actions == [ActionButton("Sign In to Your Substrate…", .signIn)])
    }

    @Test func anEndedAccessIsTheSnapshotsAndTheScreensAndASignOutMovesTheGeneration() async throws {
        let (controller, environment) = link(MemoryStorage())
        let harness = ControllerHarness(controller, environment: environment, performsEffects: false)
        let facts = SessionFacts(server: server, repository: "ada.example.com", scopes: ["read:*"])
        await harness.deliver(.loaded(session: facts, unrevoked: [], generation: 4))
        await harness.deliver(.checked(AppStatus(issues: [.accessEnded], checkedAt: now)))
        var presented = await harness.presentation
        #expect(presented.snapshot.phase == .accessEnded)
        #expect(presented.ended?.title == "Substrate Tasks's access ended")
        #expect(presented.status.sentence == "Your access ended. Sign in again to send what waits.")
        await harness.send(.signOut)
        #expect(harness.effects.last == .signOut)
        await harness.deliver(.signedOut(generation: 5, unrevoked: [facts]))
        presented = await harness.presentation
        #expect(presented.snapshot == LinkSnapshot(phase: .signedOut, generation: 5))
    }

    @Test func aCheckRunsAtMostEveryIntervalUnlessForced() async throws {
        let (controller, environment) = link(MemoryStorage())
        let harness = ControllerHarness(controller, environment: environment, performsEffects: false)
        await harness.deliver(.loaded(session: SessionFacts(server: server, repository: "ada.example.com"), unrevoked: [], generation: 0))
        await harness.deliver(.checked(AppStatus(checkedAt: harness.clock.now)))
        let before = harness.effects.count
        await harness.send(.refresh(force: false))
        #expect(harness.effects.count == before)
        await harness.send(.refresh(force: true))
        #expect(harness.effects.last == .check)
    }

    @Test func comingBackOnlineSendsWhatWaits() async throws {
        let (controller, environment) = link(MemoryStorage())
        let harness = ControllerHarness(controller, environment: environment, performsEffects: false)
        await harness.deliver(.loaded(session: SessionFacts(server: server, repository: "ada.example.com"), unrevoked: [], generation: 0))
        await harness.deliver(.outboxRead(state: .offline("gone"), waiting: 2, parked: []))
        await harness.deliver(.network(NetworkPath(online: true)))
        #expect(harness.effects.last == .flush)
    }
}

@Suite("The fixtures")
struct ScreenFixtureTests {
    @Test func everyScreenHasAFixtureAndEveryNameIsUnique() {
        let fixtures = ScreenFixtures.all
        #expect(Set(fixtures.map(\.screen)) == Set(ScreenKind.allCases))
        #expect(Set(fixtures.map(\.name)).count == fixtures.count)
    }

    @Test func everyFixtureCrossesAsJSONAndBack() throws {
        for fixture in ScreenFixtures.all {
            let data = try JSONEncoder().encode(fixture)
            #expect(try JSONDecoder().decode(ScreenFixture.self, from: data) == fixture, "\(fixture.name)")
        }
    }
}
