import AppKit
import MatineeServerCore
import SwiftUI

extension Color {
    /// Matinee's coral.
    static let matinee = Color(red: 1, green: 0x6a / 255, blue: 0x55 / 255)
}

struct MenuActions {
    var gettingStarted: () -> Void = {}
    var settings: () -> Void = {}
    var quit: () -> Void = {}
}

/// The panel under the menu-bar icon.
struct MenuView: View {
    @ObservedObject var server: ServerController
    @ObservedObject var updates: Updates
    var actions: MenuActions

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            StatusHeader(server: server)

            Button {
                NSWorkspace.shared.open(server.localURL)
            } label: {
                Label("Open Matinee", systemImage: "play.rectangle.fill")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .tint(.matinee)
            .controlSize(.large)
            .disabled(server.state != .running && server.state != .elsewhere)

            Divider()

            VStack(alignment: .leading, spacing: 2) {
                switch server.state {
                case .running, .starting:
                    MenuRow("Restart server", systemImage: "arrow.clockwise") { server.restart() }
                    MenuRow("Stop server", systemImage: "stop.fill") { server.stop() }
                case .stopping:
                    MenuRow("Stopping…", systemImage: "stop.fill") {}.disabled(true)
                case .stopped, .failed, .elsewhere:
                    MenuRow("Start server", systemImage: "play.fill") { server.start() }
                }
                MenuRow("Show log", systemImage: "doc.text.magnifyingglass") {
                    NSWorkspace.shared.open(server.layout.logFile)
                }
                MenuRow("Getting started…", systemImage: "questionmark.circle", action: actions.gettingStarted)
                MenuRow("Settings…", systemImage: "gearshape", action: actions.settings)
                if updates.available {
                    MenuRow("Check for updates…", systemImage: "arrow.down.circle") { updates.check() }
                }
            }

            Divider()

            MenuRow("Quit Matinee Server", systemImage: "power", action: actions.quit)
        }
        .padding(14)
        .frame(width: 300)
    }
}

struct StatusHeader: View {
    @ObservedObject var server: ServerController

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "ticket.fill")
                .font(.system(size: 22))
                .foregroundStyle(Color.matinee)
                .rotationEffect(.degrees(-10))
            VStack(alignment: .leading, spacing: 3) {
                Text("Matinee Server").font(.headline)
                HStack(spacing: 6) {
                    Circle().fill(dot).frame(width: 8, height: 8)
                    Text(status).font(.subheadline).foregroundStyle(.secondary)
                }
                if server.state == .running, let url = server.networkURL {
                    Text("Other devices: \(url.absoluteString)")
                        .font(.caption).foregroundStyle(.secondary)
                        .textSelection(.enabled)
                }
                if case .failed(let reason) = server.state {
                    Text(reason).font(.caption).foregroundStyle(.secondary)
                        .lineLimit(4).fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                }
            }
        }
    }

    private var status: String {
        switch server.state {
        case .running: return String(localized: "Running on port \(String(server.port))")
        case .starting: return String(localized: "Starting…")
        case .stopping: return String(localized: "Stopping…")
        case .stopped: return String(localized: "Stopped")
        case .elsewhere: return String(localized: "Another Matinee is using port \(String(server.port))")
        case .failed: return String(localized: "Not running")
        }
    }

    private var dot: Color {
        switch server.state {
        case .running: return .green
        case .starting, .stopping: return .yellow
        case .elsewhere: return .blue
        case .stopped: return .gray
        case .failed: return .red
        }
    }
}

/// A row that looks like a menu item and highlights under the pointer.
struct MenuRow: View {
    let title: LocalizedStringKey
    let systemImage: String
    let action: () -> Void
    @State private var hover = false
    @Environment(\.isEnabled) private var enabled

    init(_ title: LocalizedStringKey, systemImage: String, action: @escaping () -> Void) {
        self.title = title
        self.systemImage = systemImage
        self.action = action
    }

    var body: some View {
        Button(action: action) {
            Label(title, systemImage: systemImage)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 8)
                .padding(.vertical, 5)
                .contentShape(Rectangle())
                .background(RoundedRectangle(cornerRadius: 5).fill(hover && enabled ? Color.primary.opacity(0.1) : .clear))
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
    }
}

/// Getting started: what to do after installing, shown on the first launch.
struct WelcomeView: View {
    @ObservedObject var server: ServerController
    @ObservedObject var settings: AppSettings
    var close: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack(spacing: 14) {
                Image(systemName: "ticket.fill")
                    .font(.system(size: 40))
                    .foregroundStyle(Color.matinee)
                    .rotationEffect(.degrees(-10))
                VStack(alignment: .leading, spacing: 4) {
                    Text("Matinee is running on this Mac").font(.title2.weight(.semibold))
                    Text("It lives in the menu bar. Here's how to fill it with your films, shows and music.")
                        .foregroundStyle(.secondary)
                }
            }

            Step(number: 1, title: "Create your account") {
                Text("Open Matinee in your browser and make the admin account. Anyone on your home network can watch without signing in unless you change that.")
                Button("Open Matinee") { NSWorkspace.shared.open(server.localURL) }
                    .buttonStyle(.borderedProminent).tint(.matinee)
                    .disabled(server.state != .running)
            }

            Step(number: 2, title: "Add folders on this Mac") {
                Text("In Matinee, go to Settings → Libraries and pick a folder, such as Movies in your home folder or a drive under /Volumes. The first time, macOS asks whether Matinee Server may read that folder: choose Allow.")
            }

            Step(number: 3, title: "Add a network share (NAS)") {
                Text("Connect the share here and Matinee Server connects it again every time it starts. It then appears under /Volumes for step 2.")
                ShareField(settings: settings)
            }

            Step(number: 4, title: "Watch everywhere") {
                Text("The Matinee apps for phones and TVs find this server on your home network by themselves.")
                Toggle("Keep this Mac awake while the server runs", isOn: $settings.keepAwake)
                Toggle("Open Matinee Server when you log in", isOn: Binding(get: { settings.openAtLogin }, set: { settings.openAtLogin = $0 }))
            }

            HStack {
                Link("Read the guide", destination: URL(string: "https://matinee.yeix.io/docs/getting-started/mac/")!)
                Spacer()
                Button("Done", action: close).keyboardShortcut(.defaultAction)
            }
        }
        .padding(24)
        .frame(width: 520)
    }
}

struct Step<Content: View>: View {
    let number: Int
    let title: LocalizedStringKey
    @ViewBuilder var content: Content

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Text("\(number)")
                .font(.callout.weight(.bold))
                .foregroundStyle(.white)
                .frame(width: 24, height: 24)
                .background(Circle().fill(Color.matinee))
            VStack(alignment: .leading, spacing: 6) {
                Text(title).font(.headline)
                content
            }
            .fixedSize(horizontal: false, vertical: true)
        }
    }
}

/// Adds a network share: connects it now and keeps it in the list.
struct ShareField: View {
    @ObservedObject var settings: AppSettings
    @State private var text = ""
    @State private var message: String?
    @State private var busy = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                TextField("Share address", text: $text, prompt: Text(verbatim: "smb://nas.local/Movies"))
                    .labelsHidden()
                    .textFieldStyle(.roundedBorder)
                    .onSubmit(add)
                Button("Connect", action: add).disabled(busy || text.isEmpty)
            }
            if let message {
                Text(message).font(.caption).foregroundStyle(.secondary)
            }
        }
    }

    private func add() {
        guard let url = ShareURL.normalize(text) else {
            message = String(localized: "Type the share's address, such as smb://nas.local/Movies.")
            return
        }
        busy = true
        message = String(localized: "Connecting…")
        Task {
            let result = await Shares.mount(url)
            busy = false
            switch result {
            case .success(let point):
                if !settings.shares.contains(url.absoluteString) { settings.shares.append(url.absoluteString) }
                text = ""
                message = point.map { String(localized: "Connected at \($0.path). Add it in Matinee from there.") }
                    ?? String(localized: "Connected. It's under /Volumes.")
            case .failure(let error):
                message = String(localized: "Couldn't connect: \(error.localizedDescription)")
            }
        }
    }
}

struct SettingsView: View {
    @ObservedObject var server: ServerController
    @ObservedObject var settings: AppSettings
    @ObservedObject var updates: Updates
    @State private var port = ""

    var body: some View {
        Form {
            Section {
                TextField("Server name", text: $settings.serverName, prompt: Text("Matinee"))
                TextField("Port", text: $port)
                    .onSubmit(savePort)
                Text("The apps show this name. Restart the server after changing either.")
                    .font(.caption).foregroundStyle(.secondary)
            }
            Section {
                Toggle("Open Matinee Server when you log in", isOn: Binding(get: { settings.openAtLogin }, set: { settings.openAtLogin = $0 }))
                Toggle("Keep this Mac awake while the server runs", isOn: $settings.keepAwake)
                    .onChange(of: settings.keepAwake) { keep in
                        Power.shared.update(serverRunning: server.state == .running, keepAwake: keep)
                    }
                if updates.available {
                    Toggle("Check for updates automatically", isOn: Binding(get: { updates.automaticallyChecks }, set: { updates.automaticallyChecks = $0 }))
                }
            }
            Section("Network shares") {
                if settings.shares.isEmpty {
                    Text("None yet. Shares you connect in Getting started are connected again whenever the server starts.")
                        .font(.caption).foregroundStyle(.secondary)
                }
                ForEach(settings.shares, id: \.self) { share in
                    HStack {
                        Text(share).textSelection(.enabled)
                        Spacer()
                        Button("Remove") { settings.shares.removeAll { $0 == share } }
                    }
                }
                ShareField(settings: settings)
            }
            Section("Files") {
                HStack {
                    Button("Show data folder") { NSWorkspace.shared.activateFileViewerSelecting([server.layout.dataDir]) }
                    Button("Edit advanced settings") { NSWorkspace.shared.open(server.layout.envFile) }
                }
                Text("Matinee's database and images live in the data folder. Advanced settings (matinee.env) take any server setting from the guide.")
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .frame(width: 480)
        .fixedSize(horizontal: false, vertical: true)
        .onAppear { port = String(settings.port) }
        .onDisappear(perform: savePort)
    }

    private func savePort() {
        if let value = Int(port.trimmingCharacters(in: .whitespaces)), (1...65535).contains(value) {
            settings.port = value
        } else {
            port = String(settings.port)
        }
    }
}
