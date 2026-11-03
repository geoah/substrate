import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime

/// Every shared screen in the states an app shows it in, made by the
/// screens' own presenters from fixed inputs: an app named Substrate Tasks
/// on ada.example.com, 2026-11-03 09:00 UTC, English (US). The gallery draws
/// each one on every system, and a fixture without a renderer fails the
/// build.
public enum ScreenFixtures {
    public static let now = Date(timeIntervalSince1970: 1_793_696_400)
    public static let locale = Locale(identifier: "en_US")
    public static let timeZone = TimeZone(identifier: "UTC")!
    static let server = URL(string: "https://ada.example.com")!
    static let host = "ada.example.com"
    static let app = "Substrate Tasks"

    public static let manifest = AppManifest(
        appName: app, clientID: "https://tasks.substrate.example.com/client.json",
        scopes: ["read:substrate.reamde.dev/tasks/*", "write:substrate.reamde.dev/tasks/*"],
        details: [.package("substrate.reamde.dev/tasks", atLeast: 2)],
        requirement: ServerRequirement(minimumVersion: "0.114.0"),
        kindLabels: ["substrate.reamde.dev/tasks/task": KindLabel(singular: "Task", plural: "Tasks"),
                     "substrate.reamde.dev/tasks/list": KindLabel(singular: "List", plural: "Lists")],
        softwareVersion: "0.120.0 (20261103.090000)")

    static let words = StatusWords(app: app, device: "this iPhone", kept: "tasks")
    static let tasksPackage = "substrate.reamde.dev/tasks"

    static var probe: ServerProbe.Result {
        ServerProbe.Result(origin: server, discovery: Discovery(server: .init(version: "v0.114.0")),
                           metadata: AuthorizationServerMetadata(issuer: server, scopesSupported: ["read:*", "write:*", "agents:*", "decide:*"],
                                                                 authorizationDetailsTypesSupported: AuthorizationDetail.Types.all),
                           verdict: .compatible, repository: host)
    }

    static var session: SessionFacts {
        SessionFacts(server: server, repository: host, scopes: manifest.scopes, expiresAt: now.addingTimeInterval(30 * 86400))
    }

    static let elsewhere = SessionFacts(server: URL(string: "https://substrate.example.org")!, repository: "grace.example.org",
                                        scopes: ["read:*"])

    static let update = [
        Change(kind: .scope, words: "See and change your lists"),
        Change(kind: .upgrade, words: "Update Tasks", detail: "Version 1 to 2 (needs 2 or later)"),
    ]

    static let shipsOld = AuthorizationSetup.Block(
        reason: "server-update", problem: "This server ships version 1; the app needs version 2. The server needs an update first.")
    static let quarantined = AuthorizationSetup.Block(
        reason: "quarantined", problem: "Your copy of this package is quarantined, so approving cannot set it up. Its provider page says why.",
        fix: "/catalog/substrate.reamde.dev%2Ftasks")

    public static var all: [ScreenFixture] {
        connect + access + review + repository + status + settings + problems + about + menu
    }

    // MARK: Connect

    static var connect: [ScreenFixture] {
        let controller = ConnectController(manifest: manifest, locator: ServerLocator(probe: ServerProbe(), resolver: NoLookups()),
                                           unrevoked: [], signIn: { _ in })
        let unrevoked = ConnectController(manifest: manifest, locator: ServerLocator(probe: ServerProbe(), resolver: NoLookups()),
                                          unrevoked: [elsewhere], signIn: { _ in })
        let browser = ConnectController(manifest: manifest, locator: ServerLocator(probe: ServerProbe(), resolver: NoLookups()),
                                        opensBrowser: true, signIn: { _ in })
        var fallback = manifest
        fallback.passwordFallback = .withoutAppAuthorization
        let password = ConnectController(manifest: fallback, locator: ServerLocator(probe: ServerProbe(), resolver: NoLookups()),
                                         hasPasswordDoor: true, signIn: { _ in })
        var noAppAccess = probe
        noAppAccess.metadata = nil
        noAppAccess.verdict = .tooOld(running: "v0.113.0", needed: nil, missing: ["give apps access of their own"])
        var old = probe
        old.verdict = .tooOld(running: "v0.113.0", needed: "0.114.0", missing: ["say which version it runs"])
        func present(_ state: ConnectState, _ controller: ConnectController = controller) -> FixturePresentation {
            .connect(controller.present(PresenterInput(state: state, now: now, locale: locale, timeZone: timeZone)))
        }
        return [
            ScreenFixture("connect-empty", present(ConnectState())),
            ScreenFixture("connect-checking", present(ConnectState(address: host, phase: .checking))),
            ScreenFixture("connect-found", present(ConnectState(address: host, phase: .found(probe)))),
            ScreenFixture("connect-signing-in", present(ConnectState(address: host, phase: .signingIn(probe)))),
            ScreenFixture("connect-problem", present(ConnectState(address: "nowhere.example.com",
                                                                  problem: "nowhere.example.com has no _substrate record, and nothing answers there.",
                                                                  retryable: true))),
            ScreenFixture("connect-unrevoked", present(ConnectState(address: host, phase: .found(probe)), unrevoked)),
            ScreenFixture("connect-too-old", present(ConnectState(address: host, phase: .found(old)))),
            ScreenFixture("connect-password", present(ConnectState(address: host, phase: .found(noAppAccess)), password)),
            ScreenFixture("connect-browser-waiting", present(ConnectState(address: host, phase: .signingIn(probe)), browser)),
            ScreenFixture("connect-sign-in-failed", present(ConnectState(address: host, phase: .found(probe),
                                                                         problem: "ada.example.com refused the sign-in: the code was already used."))),
        ]
    }

    // MARK: Access

    static var access: [ScreenFixture] {
        func banner(_ issues: [AppIssue]) -> FixturePresentation {
            .accessBanner(AccessPresenter.banner(issues: issues, appName: app, host: host, offers: Set(AccessAction.Offer.allCases), now: now,
                                                 locale: locale, timeZone: timeZone)!)
        }
        let serverBlocked = [Change(kind: .upgrade, words: "Update Tasks to version 2 or later", detail: shipsOld.problem, block: shipsOld)]
        return [
            ScreenFixture("server-too-old", .serverTooOld(.present(appName: app, host: host, running: "v0.113.0", needed: "0.114.0",
                                                                   missing: ["give apps access of their own", "set up an app's account"],
                                                                   tryAgain: true, changeServer: true))),
            ScreenFixture("banner-server-update", banner([.serverTooOld(running: "v0.114.0", needed: nil, missing: [], blocked: serverBlocked)])),
            ScreenFixture("banner-needs-approval", banner([.needsApproval(changes: update)])),
            ScreenFixture("banner-blocked", banner([.needsApproval(changes: [
                Change(kind: .install, words: "Install People", detail: "Version 3"),
                Change(kind: .upgrade, words: "Update Tasks", detail: quarantined.problem, block: quarantined),
            ])])),
            ScreenFixture("banner-ends-soon", banner([.accessEndsSoon(now.addingTimeInterval(3 * 86400))])),
            ScreenFixture("banner-legacy", banner([.legacyAccess])),
            ScreenFixture("banner-unreachable", banner([.unreachable(message: "The network connection was lost.")])),
            ScreenFixture("banner-several", banner([
                .serverTooOld(running: "v0.114.0", needed: nil, missing: [], blocked: serverBlocked),
                .needsApproval(changes: update), .accessEndsSoon(now.addingTimeInterval(3 * 86400)),
            ])),
            ScreenFixture("access-ended", .accessEnded(AccessPresenter.ended(appName: app, host: host))),
        ]
    }

    static var review: [ScreenFixture] {
        let planned = manifest.plannedChanges()
        return [
            ScreenFixture("review-first-sign-in", .review(ReviewPresenter.present(appName: app, host: host, changes: planned, heading: .firstSignIn,
                                                                                  alreadyAllowed: false, setsUp: true))),
            ScreenFixture("review-update", .review(ReviewPresenter.present(appName: app, host: host, changes: update,
                                                                           heading: ReviewHeading(changes: update, firstSignIn: false),
                                                                           alreadyAllowed: false, setsUp: true))),
            ScreenFixture("review-change-access", .review(ReviewPresenter.present(
                appName: app, host: host,
                changes: [Change(kind: .scope, words: "See and change your lists", leftOut: true)] + planned.filter(\.isSetup),
                heading: .changeAccess, alreadyAllowed: true, setsUp: true))),
        ]
    }

    // MARK: Repository and status

    static var repositoryPresentation: RepositoryPresentation {
        RepositoryPresenter.present(session: session, manifest: manifest, serverVersion: "v0.114.0", unrevoked: [], offersChangeAccess: true,
                                    locale: locale, timeZone: timeZone)
    }

    static var repository: [ScreenFixture] {
        [
            ScreenFixture("repository", .repository(repositoryPresentation)),
            ScreenFixture("repository-unrevoked", .repository(RepositoryPresenter.present(
                session: session, manifest: manifest, serverVersion: "v0.114.0-3-gabc1234",
                unrevoked: [SessionFacts(server: server, repository: host), elsewhere], offersChangeAccess: true, locale: locale,
                timeZone: timeZone))),
            ScreenFixture("repository-password", .repository(RepositoryPresenter.present(
                session: SessionFacts(server: server, repository: host), manifest: manifest, serverVersion: "v0.113.0", unrevoked: [],
                offersChangeAccess: true, locale: locale, timeZone: timeZone))),
        ]
    }

    public static let refusedWrite = ParkedWrite(id: UUID(uuidString: "00000000-0000-7000-8000-000000000001")!, method: .put,
                                          kind: "substrate.reamde.dev/tasks/task", recordID: "0196f7d2-buy-milk",
                                          words: "props.status: expected one of todo, doing, done", conflict: false)

    static func tasks(_ held: HeldPackage?) -> PackageStanding {
        PackageStanding(id: tasksPackage, name: "Tasks", held: held, needs: 2)
    }

    static func status(_ status: LinkStatus, packages: [PackageStanding] = [tasks(.current(held: 2))], sending: Bool = false,
                       waiting: Int = 0, parked: [ParkedWrite] = []) -> StatusPresentation
    {
        StatusPresenter.present(status, words: words, host: host, packages: packages, sending: sending, waiting: waiting,
                                parked: parked, kindLabels: manifest.kindLabels.mapValues(\.singular), locale: locale, timeZone: timeZone)
    }

    static let diskFull = "The disk is full: 0 bytes are free on this iPhone."

    /// The status as `LinkStatus.derive` reaches it from these inputs, so a
    /// fixture is a state the link can be in.
    static func derived(packages: [PackageStanding] = [tasks(.current(held: 2))], outbox: OutboxState = .sent, waiting: Int = 0,
                        issues: [AppIssue] = [], notKept: String? = nil) -> LinkStatus
    {
        LinkStatus.derive(signedIn: true, packages: packages, outbox: outbox, waiting: waiting, issues: issues, notKept: notKept)
    }

    static var status: [ScreenFixture] {
        [
            ScreenFixture("status-up-to-date", .status(status(.ready(waiting: 0)))),
            ScreenFixture("status-sending", .status(status(.ready(waiting: 3), sending: true, waiting: 3))),
            ScreenFixture("status-offline", .status(status(.offline(waiting: 2, reason: "The network connection was lost."), waiting: 2))),
            ScreenFixture("status-refused", .status(status(.refused(waiting: 1, reason: "props.status: expected one of todo, doing, done"),
                                                           waiting: 1, parked: [refusedWrite]))),
            ScreenFixture("status-needs-approval", .status(status(.needsApproval, waiting: 1))),
            ScreenFixture("status-package-behind", .status(status(derived(packages: [tasks(.behind(held: 1))]),
                                                                  packages: [tasks(.behind(held: 1))]))),
            ScreenFixture("status-packages-behind", .status(status(derived(packages: twoBehind), packages: twoBehind))),
            ScreenFixture("status-signed-out", .status(status(.signedOut, waiting: 4))),
            ScreenFixture("status-local", .status(status(.local, packages: []))),
            ScreenFixture("status-ends-soon", .status(status(derived(waiting: 2, issues: [.accessEndsSoon(now.addingTimeInterval(3 * 86400))]),
                                                             waiting: 2))),
            ScreenFixture("status-not-kept", .status(status(derived(waiting: 1, notKept: diskFull), waiting: 1))),
            ScreenFixture("status-server-too-old", .status(status(derived(waiting: 3, issues: [
                .serverTooOld(running: "v0.113.0", needed: "0.114.0", missing: ["give apps access of their own"]),
            ]), waiting: 3))),
            ScreenFixture("status-preparing", .status(status(derived(packages: [tasks(nil)]), packages: [tasks(nil)], sending: true))),
        ]
    }

    static var twoBehind: [PackageStanding] {
        [tasks(.behind(held: 1)), PackageStanding(id: "samples.substrate.reamde.dev/people", name: "People", held: .behind(held: 2), needs: 3)]
    }

    // MARK: Settings, problems and about

    static var settings: [ScreenFixture] {
        let about = AboutPresentation.present(appName: app, version: "0.120.0-pre.20261103.090000+ac85863e", channel: "pre", offered: nil)
        let values = SettingsValues(toggles: ["reminders": true], choices: ["sort": "due"])
        let app = SettingsSection(id: .app("tasks"), title: "Tasks", rows: [
            SettingsRow(id: .app("reminders"), label: "Remind me when a task is due", control: .toggle(values.toggle("reminders"))),
            SettingsRow(id: .app("sort"), label: "Sort by", control: .choice(selected: values.choice("sort", default: "due"), options: [
                SettingsOption(id: "due", label: "Due date"), SettingsOption(id: "priority", label: "Priority"),
                SettingsOption(id: "name", label: "Name"),
            ])),
            SettingsRow(id: .app("lists"), label: "Lists", control: .link),
        ])
        let usage = [AreaUsage(area: .outbox, bytes: 2400, files: 1), AreaUsage(area: .records, bytes: 1_840_000, files: 1),
                     AreaUsage(area: .cache, bytes: 12_300_000, files: 48)]
        func settings(_ status: StatusPresentation) -> FixturePresentation {
            .settings(SettingsPresentation(sections: [app] + SettingsPresenter.status(status) + SettingsPresenter.repository(repositoryPresentation)
                + [SettingsPresenter.storage(usage, budget: 100_000_000), SettingsPresenter.about(about)]))
        }
        return [
            ScreenFixture("settings", settings(status(.ready(waiting: 0)))),
            ScreenFixture("settings-journal-problem", settings(status(derived(waiting: 1, notKept: "The journal could not be written: "
                    + "the disk is full."), waiting: 1))),
            ScreenFixture("settings-refused", settings(status(.refused(waiting: 1, reason: refusedWrite.words ?? ""), waiting: 1,
                                                              parked: [refusedWrite]))),
        ]
    }

    static var problems: [ScreenFixture] {
        [
            ScreenFixture("problem-signed-out", .problem(ProblemPresenter.present(SessionExpired(message: nil), title: "Couldn't send your changes",
                                                                                  app: app, server: server))),
            ScreenFixture("problem-offline", .problem(ProblemPresenter.present(URLError(.notConnectedToInternet), title: "Couldn't reach \(host)",
                                                                               app: app, server: server))),
            ScreenFixture("problem-plain", .problem(ProblemPresentation(title: "Not saved", message: "The disk is full."))),
        ]
    }

    static var about: [ScreenFixture] {
        [
            ScreenFixture("about", .about(.present(appName: app, version: "0.120.0", channel: "release", offered: nil))),
            ScreenFixture("about-update", .about(.present(appName: app, version: "0.120.0-pre.20261103.090000+ac85863e", channel: "pre",
                                                          offered: "0.121.0-pre.20261104.090000+9f1e2d3c"))),
        ]
    }
}

extension ScreenFixtures {
    // MARK: The app menu

    static func menu(selected: String, todayBadge: String? = "3") -> AppMenuPresentation {
        AppMenuPresentation(title: app, sections: [
            AppMenuSection(id: "tasks", items: [
                AppMenuItem(id: "today", title: "Today", symbol: .today, badge: todayBadge),
                AppMenuItem(id: "upcoming", title: "Upcoming", symbol: .upcoming),
                AppMenuItem(id: "lists", title: "Lists", symbol: .list),
            ]),
            AppMenuSection(id: "app", title: "Substrate Tasks", items: [
                AppMenuItem(id: "settings", title: "Settings", symbol: .settings),
            ]),
        ], selected: selected)
    }

    static var menu: [ScreenFixture] {
        [
            ScreenFixture("menu", .menu(menu(selected: "today"))),
            ScreenFixture("menu-settings", .menu(menu(selected: "settings", todayBadge: nil))),
        ]
    }
}

/// A resolver for fixtures, which never look anything up.
private struct NoLookups: TXTResolver {
    func txt(_ name: String) async throws -> TXTAnswer { .failed("No lookups in a fixture.") }
}
