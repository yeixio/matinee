import AppKit
import Sparkle
import SwiftUI

@main
struct MatineeServerApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate

    var body: some Scene {
        MenuBarExtra {
            MenuView(server: delegate.server, updates: delegate.updates, actions: delegate.menuActions)
        } label: {
            MenuBarIcon(server: delegate.server)
        }
        .menuBarExtraStyle(.window)
    }
}

struct MenuBarIcon: View {
    @ObservedObject var server: ServerController

    var body: some View {
        Image(systemName: server.state == .running ? "ticket.fill" : "ticket")
            .accessibilityLabel(Text("Matinee Server"))
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    let settings = AppSettings()
    lazy var server = ServerController(settings: settings)
    let updates = Updates()
    lazy var windows = Windows(app: self)

    var menuActions: MenuActions {
        MenuActions(
            gettingStarted: { [weak self] in self?.windows.showWelcome() },
            settings: { [weak self] in self?.windows.showSettings() },
            quit: { NSApp.terminate(nil) }
        )
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        // `--snapshots <folder>` draws the windows into PNGs for the guide.
        let args = CommandLine.arguments
        if let i = args.firstIndex(of: "--snapshots"), i + 1 < args.count {
            Snapshots.write(to: URL(fileURLWithPath: args[i + 1]), app: self)
            NSApp.terminate(nil)
            return
        }
        server.start()
        if !settings.welcomed {
            windows.showWelcome()
        }
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        guard server.isRunning || server.state == .stopping else { return .terminateNow }
        server.stopForQuit { NSApp.reply(toApplicationShouldTerminate: true) }
        return .terminateLater
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        // Opening the app again (from Finder or Spotlight) shows Getting started.
        if !flag { windows.showWelcome() }
        return true
    }
}

/// Sparkle, when this build was made with an update key. Builds without one
/// (from source, or in CI for a pull request) don't look for updates.
@MainActor
final class Updates: ObservableObject {
    private let controller: SPUStandardUpdaterController?

    init() {
        let key = Bundle.main.object(forInfoDictionaryKey: "SUPublicEDKey") as? String ?? ""
        controller = key.isEmpty ? nil : SPUStandardUpdaterController(startingUpdater: true, updaterDelegate: nil, userDriverDelegate: nil)
    }

    var available: Bool { controller != nil }

    var automaticallyChecks: Bool {
        get { controller?.updater.automaticallyChecksForUpdates ?? false }
        set {
            objectWillChange.send()
            controller?.updater.automaticallyChecksForUpdates = newValue
        }
    }

    func check() {
        NSApp.activate(ignoringOtherApps: true)
        controller?.checkForUpdates(nil)
    }
}

/// The Getting started and Settings windows. They're plain AppKit windows so
/// the menu-bar app can open them at launch.
@MainActor
final class Windows {
    private unowned let app: AppDelegate
    private var welcome: NSWindow?
    private var settings: NSWindow?

    init(app: AppDelegate) { self.app = app }

    func showWelcome() {
        welcome = show(welcome, title: String(localized: "Welcome to Matinee")) {
            WelcomeView(server: self.app.server, settings: self.app.settings, close: { [weak self] in
                self?.app.settings.welcomed = true
                self?.welcome?.close()
            })
        }
    }

    func showSettings() {
        settings = show(settings, title: String(localized: "Matinee Server Settings")) {
            SettingsView(server: self.app.server, settings: self.app.settings, updates: self.app.updates)
        }
    }

    private func show<V: View>(_ existing: NSWindow?, title: String, @ViewBuilder content: () -> V) -> NSWindow {
        NSApp.activate(ignoringOtherApps: true)
        if let existing {
            existing.makeKeyAndOrderFront(nil)
            return existing
        }
        let window = NSWindow(contentViewController: NSHostingController(rootView: content()))
        window.title = title
        window.styleMask = [.titled, .closable, .miniaturizable]
        window.isReleasedWhenClosed = false
        window.center()
        window.makeKeyAndOrderFront(nil)
        return window
    }
}
