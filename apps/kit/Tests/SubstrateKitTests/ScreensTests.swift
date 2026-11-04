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

@Suite("The Access screen", .timeLimit(.minutes(10)))
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

@Suite("The Connect screen", .timeLimit(.minutes(10)))
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
        #expect(presented.footer?.found == LabeledValue("Server found", "ada.example.com", verbatim: true))
        #expect(presented.footer?.repository == LabeledValue("Repository", "ada.example.com", verbatim: true))
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

@Suite("The Repository and Status screens", .timeLimit(.minutes(10)))
struct RepositoryStatusTests {
    let words = StatusWords(app: "Tasks", device: "this iPhone", kept: "tasks")

    func package(_ held: HeldPackage?, _ id: String = "substrate.reamde.dev/tasks", needs: Int = 2) -> PackageStanding {
        PackageStanding(id: id, name: words.packageName(id), held: held, needs: needs)
    }

    @Test func anAppSessionListsEachScopeAndWhenItEnds() {
        let session = SessionFacts(server: URL(string: "https://ada.example.com")!, repository: "ada.example.com",
                                   scopes: ["read:*", "write:substrate.reamde.dev/tasks/*"], expiresAt: now)
        let repository = RepositoryPresenter.present(session: session, manifest: nil, serverVersion: "v0.114.0", unrevoked: [],
                                                     offersChangeAccess: true, locale: enUS, timeZone: utc)
        #expect(repository.title == "Repository")
        #expect(repository.rows == [LabeledValue("Server", "ada.example.com"), LabeledValue("Repository", "ada.example.com"),
                                    LabeledValue("Server version", "0.114.0")])
        #expect(repository.approved?.title == "Approved access")
        #expect(repository.approved?.scopes.map(\.scope) == ["read:*", "write:substrate.reamde.dev/tasks/*"])
        #expect(repository.approved?.until.value.hasPrefix("Nov 3, 2026 at 9:00") == true)
        #expect(repository.changeAccess == "Change Access…")
        #expect(repository.footer == "Sign Out: signs out on this device and revokes its access on ada.example.com.")
        #expect(RepositoryPresenter.present(session: session, manifest: nil, serverVersion: nil, unrevoked: [], offersChangeAccess: true,
                                            locale: enUS, timeZone: utc).title == "Repository")
    }

    @Test func aPasswordSessionReachesEverything() {
        let repository = RepositoryPresenter.present(session: SessionFacts(server: URL(string: "https://ada.example.com")!,
                                                                           repository: "ada.example.com"),
                                                     manifest: nil, serverVersion: nil, unrevoked: [], offersChangeAccess: true, locale: enUS,
                                                     timeZone: utc)
        #expect(repository.access == LabeledValue("Access", "Everything, with your password"))
        #expect(repository.changeAccess == nil && repository.approved == nil)
        #expect(repository.footer.contains("revokes its sign-in"))
    }

    @Test func theStatusIsWhatBlocksMostInOrder() {
        let current = [package(.current(held: 2))]
        #expect(LinkStatus.derive(signedIn: false, packages: [package(nil)], outbox: .idle, waiting: 0) == .local)
        #expect(LinkStatus.derive(signedIn: true, packages: current, outbox: .signedOut, waiting: 2) == .signedOut)
        #expect(LinkStatus.derive(signedIn: true, packages: current, outbox: .idle, waiting: 0, issues: [.needsApproval(changes: [])])
            == .needsApproval)
        #expect(LinkStatus.derive(signedIn: true, packages: current, outbox: .idle, waiting: 0, notKept: "full") == .notKept("full"))
        #expect(LinkStatus.derive(signedIn: true, packages: [package(nil)], outbox: .idle, waiting: 0) == .preparing(["Tasks"]))
        #expect(LinkStatus.derive(signedIn: true, packages: [package(.behind(held: 1))], outbox: .idle, waiting: 0)
            == .packagesBehind([package(.behind(held: 1))]))
        #expect(LinkStatus.derive(signedIn: true, packages: current, outbox: .offline("x"), waiting: 3) == .offline(waiting: 3, reason: "x"))
        #expect(LinkStatus.derive(signedIn: true, packages: current, outbox: .sent, waiting: 0) == .ready(waiting: 0))
        #expect(LinkStatus.derive(signedIn: true, packages: [], outbox: .sent, waiting: 0) == .ready(waiting: 0))
    }

    @Test func eachPackageIsDerivedAndWordedOnItsOwn() {
        // Every package, not the first: the second one is behind.
        let tasks = package(.current(held: 2))
        let people = package(.behind(held: 2), "samples.substrate.reamde.dev/people", needs: 3)
        let behind = LinkStatus.derive(signedIn: true, packages: [tasks, people], outbox: .sent, waiting: 0)
        #expect(behind == .packagesBehind([people]))
        #expect(words.sentence(behind, locale: enUS, timeZone: utc)
            == "Your substrate has version 2 of the People package and this app needs version 3 or later. "
            + "The update waits for your approval on your substrate; tasks wait on this iPhone until then.")
        let both = LinkStatus.derive(signedIn: true, packages: [package(.behind(held: 1)), people], outbox: .sent, waiting: 0)
        #expect(words.sentence(both, locale: enUS, timeZone: utc).hasPrefix("Your substrate has older versions of the Tasks and People packages"))
        let unread = LinkStatus.derive(signedIn: true, packages: [package(nil), package(nil, "samples.substrate.reamde.dev/people")],
                                       outbox: .sent, waiting: 0)
        #expect(words.sentence(unread, locale: enUS, timeZone: utc) == "Checking the Tasks and People packages on your substrate.")
        let status = StatusPresenter.present(both, words: words, host: "ada.example.com", packages: [package(.behind(held: 1)), people],
                                             sending: false, waiting: 0, parked: [], locale: enUS, timeZone: utc)
        #expect(status.packages.map(\.row) == [LabeledValue("Tasks package", "Version 1, waiting for the update to version 2"),
                                                LabeledValue("People package", "Version 2, waiting for the update to version 3")])
    }

    @Test func aRefusedWriteIsListedWithRetryAndDiscardAndItsWordsStayApart() {
        let write = ParkedWrite(id: UUID(), method: .patch, kind: "substrate.reamde.dev/tasks/task", recordID: "t1",
                                words: "props.status: expected one of todo, doing, done", conflict: false)
        let status = StatusPresenter.present(.refused(waiting: 1, reason: "props.status: expected one of todo, doing, done"), words: words,
                                             host: "ada.example.com", packages: [package(.current(held: 2))], sending: false, waiting: 1,
                                             parked: [write], kindLabels: ["substrate.reamde.dev/tasks/task": "Task"], locale: enUS,
                                             timeZone: utc)
        #expect(status.sentence == "Your substrate refused a change. 1 change waits; retry or discard it below.")
        #expect(status.problem == nil)
        // What the write was of and what was refused are two facts.
        #expect(status.refused?.rows.map(\.label) == ["Task"])
        #expect(status.refused?.rows.map(\.status) == ["Change refused"])
        #expect(status.refused?.rows.first?.words == "props.status: expected one of todo, doing, done")
        #expect(status.refused?.rows.first?.retry == ActionButton("Retry", .retry(write.id)))
        #expect(status.actions.map(\.title) == ["Send Now"])
        let unlisted = StatusPresenter.present(.refused(waiting: 1, reason: "no"), words: words, host: nil, packages: [], sending: false,
                                               waiting: 1, parked: [], locale: enUS, timeZone: utc)
        #expect(unlisted.problem?.message == "no")
    }

    @Test func anAppsWordsReplaceTheFooters() {
        var mine = words
        mine.footer = "Sessions are written to {server} when they finish."
        mine.refusedFooter = "Discard gives up the rest of its session's changes."
        let write = ParkedWrite(id: UUID(), method: .put, kind: "substrate.reamde.dev/training/session", recordID: "s1", words: "no",
                                conflict: false)
        let status = StatusPresenter.present(.refused(waiting: 1, reason: "no"), words: mine, host: "ada.example.com", packages: [],
                                             sending: false, waiting: 1, parked: [write],
                                             kindLabels: ["substrate.reamde.dev/training/session": "Training session"], locale: enUS,
                                             timeZone: utc)
        #expect(status.footer == "Sessions are written to ada.example.com when they finish.")
        #expect(status.refused?.footer == "Discard gives up the rest of its session's changes.")
        #expect(status.refused?.rows.map(\.label) == ["Training session"])
        #expect(status.refused?.rows.map(\.status) == ["Write refused"])
        let plain = StatusPresenter.present(.ready(waiting: 0), words: words, host: nil, packages: [], sending: false, waiting: 0, parked: [],
                                            locale: enUS, timeZone: utc)
        #expect(plain.footer == "Changes are written to your substrate as you make them, and wait on this iPhone while it cannot be reached.")
    }

    @Test func aChangeNotKeptSaysWhyApart() {
        let status = StatusPresenter.present(.notKept("The disk is full."), words: words, host: nil, packages: [], sending: false,
                                             waiting: 0, parked: [], locale: enUS, timeZone: utc)
        #expect(status.sentence == "A change could not be kept on this iPhone. Tasks already kept stay.")
        #expect(status.problem == ProblemPresentation(title: "Not kept on this iPhone", message: "The disk is full."))
    }

    @Test func settingsRowsAreNamedByWhatTheyAreAndEachScopeIsARow() throws {
        let fixture = try #require(ScreenFixtures.all.first { $0.name == "settings-refused" })
        guard case let .settings(settings) = fixture.presentation else { throw Refused(description: "not settings") }
        let ids = settings.sections.flatMap { $0.rows.map(\.id) }
        #expect(Set(ids).count == ids.count)
        #expect(settings.sections.map(\.id) == [.app("tasks"), .status, .refused, .repository, .approvedAccess, .storage, .about])
        let refused = try #require(settings.sections.first { $0.id == .refused })
        let write = ScreenFixtures.refusedWrite
        #expect(refused.rows.map(\.id) == [.status(.refused(write.id)), .status(.refusedStatus(write.id)), .status(.refusedKind(write.id)),
                                            .status(.refusedRecord(write.id)), .status(.action(.retry(write.id))),
                                            .status(.action(.discard(write.id)))])
        #expect(refused.rows[0].detail == write.words)
        #expect(refused.rows[2].label == "Kind" && refused.rows[2].control == .value(write.kind, verbatim: true))
        #expect(refused.rows[3].label == "Record ID" && refused.rows[3].control == .value(write.recordID, verbatim: true))
        let approved = try #require(settings.sections.first { $0.id == .approvedAccess })
        #expect(approved.rows.map(\.id) == ScreenFixtures.manifest.scopes.map { .repository(.scope($0)) } + [.repository(.until), .repository(.changeAccess)])
        #expect(approved.rows.allSatisfy { !$0.label.contains("\n") })
        let status = try #require(settings.sections.first { $0.id == .status })
        #expect(status.rows.contains { $0.id == .status(.action(.sendNow)) })
        // A row's id crosses as JSON without its words.
        let encoded = String(decoding: try JSONEncoder().encode(SettingsRowID.status(.action(.sendNow))), as: UTF8.self)
        #expect(!encoded.contains("Send Now"))
    }

    @Test func aSizeReadsInDecimalUnits() {
        #expect(SettingsPresenter.byteWords(0) == "0 bytes")
        #expect(SettingsPresenter.byteWords(12_300) == "12.3 KB")
        #expect(SettingsPresenter.byteWords(1_000_000) == "1 MB")
        #expect(SettingsPresenter.byteWords(345_600_000) == "346 MB")
    }

    @Test func aSettingsCommandMovesItsValue() {
        let values = SettingsValues().applying(.toggle(row: .app("reminders"), on: true)).applying(.choose(row: .app("sort"), option: "name"))
        #expect(values.toggle("reminders") && values.choice("sort", default: "due") == "name")
        #expect(values.applying(.perform(row: .storage(.clear))) == values)
    }

    @Test func aStorageClearIsOfferedOnlyForWhatCanBeReadAgain() {
        let waitingOnly = SettingsPresenter.storage([AreaUsage(area: .outbox, bytes: 10, files: 1)], budget: nil)
        #expect(waitingOnly.rows.last?.control == .action(destructive: true, enabled: false))
    }

    @Test func theBudgetIsSaidOnceUnderEachAreasSize() {
        let section = SettingsPresenter.storage([AreaUsage(area: .cache, bytes: 512, files: 1), AreaUsage(area: .records, bytes: 85, files: 1)],
                                                budget: 100 * 1024 * 1024)
        let values = section.rows.compactMap { row -> String? in if case let .value(value, _) = row.control { value } else { nil } }
        #expect(values == ["512 bytes", "85 bytes", "597 bytes of 105 MB"])
        #expect(section.rows.map(\.id) == [.storage(.area(.cache)), .storage(.area(.records)), .storage(.total), .storage(.clear)])
        #expect(section.rows[2].label == "Total")
        #expect(SettingsPresenter.storage([AreaUsage(area: .cache, bytes: 512, files: 1)], budget: nil).rows[1].control
            == .value("512 bytes", verbatim: false))
    }

    @Test func signOutAsksFromItsOwnRowInTheAppsWords() throws {
        let session = SessionFacts(server: URL(string: "https://ada.example.com")!, repository: "ada.example.com", scopes: ["write:example.com/notes/*"])
        let repository = RepositoryPresenter.present(session: session, manifest: ScreenFixtures.manifest, serverVersion: nil, unrevoked: [],
                                                     offersChangeAccess: true, signOutMessage: "This iPhone forgets its saved notes.",
                                                     locale: Locale(identifier: "en_US"), timeZone: TimeZone(identifier: "UTC")!)
        let confirmation = try #require(repository.signOutConfirmation)
        #expect(confirmation.question == .repository(.signOut))
        #expect(confirmation.title == "Sign out of ada.example.com?")
        #expect(confirmation.message == "\(ScreenFixtures.manifest.appName)'s access on ada.example.com is revoked. This iPhone forgets its saved notes.")
        let row = try #require(SettingsPresenter.repository(repository)[0].rows.first { $0.id == .repository(.signOut) })
        #expect(row.confirmation == confirmation)
    }

    @Test func theStatusIsHeadedByWhatItIsAboutAndTheVersionReadsAsEveryValue() {
        let status = StatusPresenter.present(.ready(waiting: 0), words: StatusWords(app: "Notes", device: "this iPhone", kept: "notes"), host: "ada.example.com",
                                             packages: [], sending: false, waiting: 0, parked: [], locale: Locale(identifier: "en_US"),
                                             timeZone: TimeZone(identifier: "UTC")!)
        #expect(status.title == "Your Substrate")
        #expect(status.sentence == "Up to date.")
        let about = AboutPresentation.present(appName: "Notes", version: "1.2.0-pre.3 (41)", channel: "pre", offered: nil)
        #expect(SettingsPresenter.about(about).rows[0].control == .value("1.2.0-pre.3 (41)", verbatim: false))
    }
}

@Suite("The link", .timeLimit(.minutes(10)))
struct LinkControllerTests {
    let secret = "sub_app_never_shown_9f1e"
    let server = URL(string: "https://ada.example.com")!
    let words = StatusWords(app: "Substrate Tasks", device: "this iPhone", kept: "tasks")

    func environment(folder: RepositoryFolder? = nil, transport: any HTTPTransport = StubTransport(),
                     connectivity: ScriptedConnectivity = ScriptedConnectivity(), clock: TestClock = TestClock()) -> AppEnvironment
    {
        var environment = AppEnvironment.testing(clock: clock, transport: transport)
        environment.devices.connectivity = connectivity
        environment.folder = folder
        return environment
    }

    func link(_ storage: MemoryStorage, packages: [String: Int] = ["substrate.reamde.dev/tasks": 2],
              outbox: Outbox? = nil) -> LinkController<NoApp>
    {
        LinkController(manifest: ScreenFixtures.manifest, sessions: SessionStore(storage: storage), outbox: outbox, packages: packages,
                       words: words)
    }

    func signedIn() throws -> MemoryStorage {
        let storage = MemoryStorage()
        let session = Session(server: server, repository: "ada.example.com", secret: secret, clientID: "https://tasks.example.com/client.json",
                              scopes: ["read:*"])
        try SessionStore(storage: storage).save(session)
        return storage
    }

    @Test func itOpensOnTheStoredSessionOnItsOwnAndItsSnapshotCarriesNoSecret() async throws {
        let harness = ControllerHarness(link(try signedIn()), environment: environment(folder: try RepositoryFolder(root: temporaryFolder())))
        // No command: the link's event at open loads the session.
        try await harness.until { harness in
            let presented = await harness.presentation
            return presented.snapshot.online == true && presented.repository != nil
        }
        let presented = await harness.presentation
        #expect(presented.snapshot.phase == .signedIn)
        #expect(presented.snapshot.repository == "ada.example.com")
        #expect(presented.repository?.server == LabeledValue("Server", "ada.example.com"))
        #expect(presented.app == NoApp.State())
        let state = await harness.state
        let encoded = String(decoding: try JSONEncoder().encode(presented), as: UTF8.self)
            + String(decoding: try JSONEncoder().encode(state), as: UTF8.self)
        #expect(!encoded.contains(secret))
    }

    @Test func signedOutItSaysSoAndOffersTheWayIn() async throws {
        let harness = ControllerHarness(link(MemoryStorage()), environment: environment())
        try await harness.until { await $0.presentation.snapshot.online == true }
        let presented = await harness.presentation
        #expect(presented.snapshot.phase == .signedOut)
        #expect(presented.repository == nil)
        #expect(presented.status.sentence == "Not signed in. Tasks stay on this iPhone.")
        #expect(presented.status.actions == [ActionButton("Sign In to Your Substrate…", .signIn)])
    }

    @Test func anEndedAccessIsTheSnapshotsAndTheScreensAndASignOutMovesTheGeneration() async throws {
        let harness = ControllerHarness(link(MemoryStorage()), environment: environment(), performsEffects: false)
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

    @Test func aSignOutRevokesTheTokenAndOnlyOneTheServerRefusedWaits() async throws {
        let storage = try signedIn()
        let stub = StubTransport().on("POST", "/oauth/revoke")
        let harness = ControllerHarness(link(storage), environment: environment(transport: stub))
        try await harness.until { await $0.presentation.snapshot.phase == .signedIn }
        await harness.send(.signOut)
        try await harness.until { await $0.presentation.snapshot.phase == .signedOut }
        #expect(stub.sent("POST", "/oauth/revoke").count == 1)
        #expect(await harness.state.unrevoked.isEmpty)
        #expect(SessionStore(storage: storage).pendingRevocations().isEmpty)

        let refusing = try signedIn()
        let down = StubTransport().on("POST", "/oauth/revoke", status: 503)
        let other = ControllerHarness(link(refusing), environment: environment(transport: down))
        try await other.until { await $0.presentation.snapshot.phase == .signedIn }
        await other.send(.signOut)
        try await other.until { await $0.presentation.snapshot.phase == .signedOut }
        #expect(await other.state.unrevoked.map(\.repository) == ["ada.example.com"])
        #expect(SessionStore(storage: refusing).pendingRevocations().count == 1)
    }

    @Test func aCheckRunsAtMostEveryIntervalUnlessForced() async throws {
        let harness = ControllerHarness(link(MemoryStorage()), environment: environment(), performsEffects: false)
        await harness.deliver(.loaded(session: SessionFacts(server: server, repository: "ada.example.com"), unrevoked: [], generation: 0))
        await harness.deliver(.checked(AppStatus(checkedAt: harness.clock.now)))
        let before = harness.effects.count
        await harness.send(.refresh(force: false))
        #expect(harness.effects.count == before)
        await harness.send(.refresh(force: true))
        #expect(harness.effects.last == .check)
    }

    @Test func comingBackOnlineSendsWhatWaits() async throws {
        let harness = ControllerHarness(link(MemoryStorage()), environment: environment(), performsEffects: false)
        await harness.deliver(.loaded(session: SessionFacts(server: server, repository: "ada.example.com"), unrevoked: [], generation: 0))
        await harness.deliver(.network(NetworkPath(online: false)))
        await harness.deliver(.outboxChanged(OutboxFacts(state: .offline("gone"), waiting: 2, sendable: 2, revision: 3)))
        // Offline, a write queued waits.
        #expect(!harness.effects.contains(.flush))
        await harness.deliver(.network(NetworkPath(online: true)))
        #expect(harness.effects.last == .flush)
    }

    @Test func aReportOfTheOutboxsOwnFlushStartsNoOther() async throws {
        let harness = ControllerHarness(link(MemoryStorage()), environment: environment(), performsEffects: false)
        await harness.deliver(.loaded(session: SessionFacts(server: server, repository: "ada.example.com"), unrevoked: [], generation: 0))
        await harness.deliver(.outboxChanged(OutboxFacts(state: .idle, waiting: 1, sendable: 1, revision: 1)))
        #expect(harness.effects.filter { $0 == .flush }.count == 1)
        // The same revision again (the flush's own reports, a hold that
        // left a write queued) is no new write.
        await harness.deliver(.outboxChanged(OutboxFacts(state: .sent, waiting: 1, sendable: 1, revision: 1)))
        #expect(harness.effects.filter { $0 == .flush }.count == 1)
        await harness.deliver(.outboxChanged(OutboxFacts(state: .sent, waiting: 2, sendable: 2, revision: 2)))
        #expect(harness.effects.filter { $0 == .flush }.count == 2)
    }

    @Test func aWriteThroughTheOutboxMovesTheStatusAndIsSent() async throws {
        let scripted = ScriptedServer()
        let put = scripted.hold("PUT", "/api/v1/substrate.reamde.dev/tasks/task/t1")
        scripted.on { sent in
            if sent.method == "PUT" { return (200, #"{"id":"t1","kind":"substrate.reamde.dev/tasks/task","properties":{},"version":1}"#) }
            return (404, #"{"error":{"code":"not_found","message":"not here"}}"#)
        }
        let folder = try RepositoryFolder(root: temporaryFolder())
        let outbox = Outbox(folder: folder)
        let clock = TestClock()
        let harness = ControllerHarness(link(try signedIn(), packages: [:], outbox: outbox), clock: clock,
                                        environment: environment(folder: folder, transport: scripted, clock: clock))
        try await harness.until { await $0.presentation.status.sentence == "Up to date." }
        try await outbox.enqueue([OutboxEntry(method: .put, kind: "substrate.reamde.dev/tasks/task", recordID: "t1")])
        // Queued: the status says so, and a flush started on its own.
        await put.arrived()
        // A flush's reports are drawn within a window on the link's clock.
        try await harness.until { harness in
            await harness.advance(by: 0.1)
            let status = await harness.presentation.status
            return status.sentence == "Sending 1 change." && status.busy
        }
        #expect(await harness.presentation.snapshot.waiting == 1)
        put.release()
        try await harness.until { harness in
            await harness.advance(by: 0.1)
            return await harness.presentation.status.sentence == "Up to date."
        }
        #expect(await harness.presentation.snapshot.waiting == 0)
        #expect(scripted.methodsAndPaths.filter { $0.hasPrefix("PUT") } == ["PUT /api/v1/substrate.reamde.dev/tasks/task/t1"])
    }
}

/// An app under the link, small: it keeps what the link said last, its
/// requests can be answered 401 or 403, and it may keep a journal.
struct Notes: LinkedApp {
    enum Command: Codable, Equatable, Sendable {
        case fetch
        case write(String)
    }

    enum Event: Codable, Equatable, Sendable {
        case linked(LinkSnapshot.Phase, Int)
        case fetchFailed(LinkReport)
        case wrote(String)
    }

    struct State: Codable, Equatable, Sendable {
        var phases: [LinkSnapshot.Phase] = []
        var generation = 0
        var notes: [String] = []
    }

    typealias Presentation = State

    let journal: MemoryJournal<Event, State>?
    let refusal: LinkReport

    init(journal: MemoryJournal<Event, State>? = nil, refusal: LinkReport = .accessEnded) {
        self.journal = journal
        self.refusal = refusal
    }

    static func == (lhs: Notes, rhs: Notes) -> Bool { lhs.journal === rhs.journal && lhs.refusal == rhs.refusal }

    var initialState: State { State() }

    func events(for command: Command, in context: Enrichment<Notes>) -> [Event] {
        switch command {
        case .fetch: [.fetchFailed(refusal)]
        case let .write(text): [.wrote(text)]
        }
    }

    func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        switch event {
        case let .linked(phase, generation):
            next.phases.append(phase)
            next.generation = generation
        case .fetchFailed: break
        case let .wrote(text): next.notes.append(text)
        }
        return next
    }

    func journaling(of event: Event) -> Journaling {
        if case .wrote = event { return .append }
        return .skip
    }

    func present(_ input: PresenterInput<Notes>) -> State { input.state }

    func event(forLink link: LinkSnapshot) -> Event? { .linked(link.phase, link.generation) }

    func report(of event: Event) -> LinkReport? {
        if case let .fetchFailed(report) = event { return report }
        return nil
    }

    func journal(in environment: AppEnvironment) -> (any JournalStore<Event, State>)? { journal }
}

@Suite("The link and its app, one tree", .timeLimit(.minutes(10)))
struct LinkTreeTests {
    let server = URL(string: "https://ada.example.com")!
    let words = StatusWords(app: "Substrate Tasks", device: "this iPhone", kept: "notes")

    func harness(_ app: Notes) -> ControllerHarness<LinkController<Notes>> {
        let link = LinkController(app: app, manifest: ScreenFixtures.manifest, sessions: SessionStore(storage: MemoryStorage()),
                                  words: words)
        return ControllerHarness(link, performsEffects: false)
    }

    @Test func theLinksSnapshotReachesTheAppInsideTheStepThatMovedIt() async throws {
        let harness = harness(Notes())
        // At open, the app was told the link's first snapshot.
        #expect(await harness.presentation.app?.phases == [.signedOut])
        harness.clearTrace()
        await harness.deliver(.loaded(session: SessionFacts(server: server, repository: "ada.example.com", scopes: ["read:*"]),
                                      unrevoked: [], generation: 2))
        let presented = await harness.presentation
        #expect(presented.app?.phases == [.signedOut, .signedIn])
        #expect(presented.app?.generation == 2)
        // The link's step and the app's went out as one tree.
        #expect(harness.published == 1)
    }

    @Test func aRequestAnswered401InTheAppEndsTheLinksAccess() async throws {
        let harness = harness(Notes(refusal: .accessEnded))
        await harness.deliver(.loaded(session: SessionFacts(server: server, repository: "ada.example.com", scopes: ["read:*"]),
                                      unrevoked: [], generation: 0))
        try await harness.runtime.send(Notes.Command.fetch, to: LinkController<Notes>.appPath)
        let presented = await harness.presentation
        #expect(presented.snapshot.phase == .accessEnded)
        #expect(presented.ended != nil)
        #expect(presented.status.sentence == "Your access ended. Sign in again to send what waits.")
        // And the app heard the ended access in the same step.
        #expect(presented.app?.phases.last == .accessEnded)
        // A new sign-in clears it.
        await harness.deliver(.loaded(session: SessionFacts(server: server, repository: "ada.example.com", scopes: ["read:*"]),
                                      unrevoked: [], generation: 0))
        #expect(await harness.presentation.snapshot.phase == .signedIn)
    }

    @Test func aRequestAnswered403InTheAppAsksForApprovalAndChecksAgain() async throws {
        let harness = harness(Notes(refusal: .needsApproval))
        await harness.deliver(.loaded(session: SessionFacts(server: server, repository: "ada.example.com", scopes: ["read:*"]),
                                      unrevoked: [], generation: 0))
        harness.clearTrace()
        try await harness.runtime.send(Notes.Command.fetch, to: LinkController<Notes>.appPath)
        #expect(harness.effects == [.check])
        #expect(await harness.presentation.status.sentence == "Substrate Tasks needs your approval for more access. Review it to send what waits.")
    }

    @Test func aJournalTheAppCannotWriteIsAChangeNotKept() async throws {
        let journal = MemoryJournal<Notes.Event, Notes.State>()
        let harness = harness(Notes(journal: journal))
        await harness.deliver(.loaded(session: SessionFacts(server: server, repository: "ada.example.com", scopes: ["read:*"]),
                                      unrevoked: [], generation: 0))
        journal.failWrites(with: Refused(description: "No space left"))
        try await harness.runtime.send(Notes.Command.write("milk"), to: LinkController<Notes>.appPath)
        let status = await harness.presentation.status
        #expect(status.sentence == "A change could not be kept on this iPhone. Notes already kept stay.")
        #expect(status.problem?.message == "The journal could not be written: No space left")
        journal.failWrites(with: nil)
        try await harness.runtime.send(Notes.Command.write("eggs"), to: LinkController<Notes>.appPath)
        #expect(await harness.presentation.status.problem == nil)
    }

    @Test func aCommandForAnotherTypeAtAPathIsRefused() async throws {
        let harness = harness(Notes())
        await #expect(throws: UnroutableCommand.self) {
            try await harness.runtime.send(Room.Command.say("hi"), to: LinkController<Notes>.appPath)
        }
    }
}

@Suite("The fixtures", .timeLimit(.minutes(10)))
struct ScreenFixtureTests {
    @Test func everyScreenHasAFixtureAndEveryNameIsUnique() {
        let fixtures = ScreenFixtures.all
        #expect(Set(fixtures.map(\.screen)) == Set(ScreenKind.allCases))
        #expect(Set(fixtures.map(\.name)).count == fixtures.count)
    }

    @Test func theStatesAnAppShowsAreEachAFixture() {
        let names = Set(ScreenFixtures.all.map(\.name))
        for name in ["status-ends-soon", "status-not-kept", "status-server-too-old", "status-preparing", "status-packages-behind",
                     "connect-password", "connect-browser-waiting", "connect-sign-in-failed", "settings-journal-problem"]
        {
            #expect(names.contains(name), "\(name)")
        }
    }

    @Test func everyFixtureCrossesAsJSONAndBack() throws {
        for fixture in ScreenFixtures.all {
            let data = try JSONEncoder().encode(fixture)
            #expect(try JSONDecoder().decode(ScreenFixture.self, from: data) == fixture, "\(fixture.name)")
        }
    }
}
