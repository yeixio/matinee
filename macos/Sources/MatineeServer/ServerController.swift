import AppKit
import Foundation
import MatineeServerCore

/// Runs the bundled Matinee server as a child process: starts it, watches it,
/// restarts it when it stops on its own and stops it when the app quits.
@MainActor
final class ServerController: ObservableObject {
    enum State: Equatable {
        case stopped
        case starting
        case running
        case stopping
        /// Another Matinee (Homebrew, Docker) already answers on the port.
        case elsewhere
        case failed(String)
    }

    @Published private(set) var state: State = .stopped
    @Published private(set) var port = 7171

    let layout: ServerLayout
    private let settings: AppSettings
    private var process: Process?
    private var log: FileHandle?
    private var healthTimer: Timer?
    private var stopRequested = false
    private var crashTimes: [Date] = []

    init(settings: AppSettings) {
        self.settings = settings
        layout = ServerLayout.standard(
            home: FileManager.default.homeDirectoryForCurrentUser,
            executables: Bundle.main.bundleURL.appendingPathComponent("Contents/MacOS")
        )
    }

    /// http://localhost:<port>, for this Mac's browser.
    var localURL: URL { URL(string: "http://localhost:\(port)")! }

    /// The address other devices on the home network use.
    var networkURL: URL? {
        guard let ip = NetworkAddress.lanIPv4() else { return nil }
        return URL(string: "http://\(ip):\(port)")
    }

    var isRunning: Bool { state == .running || state == .starting }

    func start() {
        guard process == nil, state != .starting else { return }
        stopRequested = false
        state = .starting
        let env = environment()
        port = ServerEnvironment.port(of: env, default: settings.port)

        Task {
            // A Matinee that's already listening (brew services, Docker) would
            // make ours fail on the port; say so instead.
            if await Self.healthy(port: port) {
                state = .elsewhere
                return
            }
            await Shares.mountAll(settings.shares)
            launch(env: env)
        }
    }

    func stop() {
        guard let process else {
            state = .stopped
            return
        }
        stopRequested = true
        state = .stopping
        healthTimer?.invalidate()
        process.terminate()
        // The server finishes requests in flight for up to 10 seconds; after
        // 15 it's stuck, so end it.
        let pid = process.processIdentifier
        DispatchQueue.main.asyncAfter(deadline: .now() + 15) { [weak self] in
            if self?.process?.processIdentifier == pid { kill(pid, SIGKILL) }
        }
    }

    func restart() {
        if process == nil { start(); return }
        restartAfterStop = true
        stop()
    }

    private var restartAfterStop = false
    private var stopWaiters: [() -> Void] = []

    /// Stops the server and calls done once it has exited (for quitting).
    func stopForQuit(_ done: @escaping () -> Void) {
        guard process != nil else { done(); return }
        stopWaiters.append(done)
        stop()
    }

    private func environment() -> [String: String] {
        let fm = FileManager.default
        if !fm.fileExists(atPath: layout.envFile.path) {
            try? fm.createDirectory(at: layout.dataDir, withIntermediateDirectories: true)
            try? EnvFile.template.write(to: layout.envFile, atomically: true, encoding: .utf8)
        }
        let file = (try? String(contentsOf: layout.envFile, encoding: .utf8)).map(EnvFile.parse) ?? [:]
        return ServerEnvironment.build(base: ProcessInfo.processInfo.environment, layout: layout,
                                       port: settings.port, serverName: settings.serverName, envFile: file)
    }

    private func launch(env: [String: String]) {
        let fm = FileManager.default
        try? fm.createDirectory(at: layout.dataDir, withIntermediateDirectories: true)
        try? fm.createDirectory(at: layout.logFile.deletingLastPathComponent(), withIntermediateDirectories: true)
        LogFile.rotate(layout.logFile, maxBytes: 10 << 20)
        if !fm.fileExists(atPath: layout.logFile.path) { fm.createFile(atPath: layout.logFile.path, contents: nil) }
        let log = try? FileHandle(forWritingTo: layout.logFile)
        _ = try? log?.seekToEnd()
        self.log = log

        let p = Process()
        p.executableURL = Bundle.main.bundleURL.appendingPathComponent("Contents/MacOS/matinee")
        p.environment = env
        p.currentDirectoryURL = layout.dataDir
        p.standardOutput = log ?? FileHandle.nullDevice
        p.standardError = log ?? FileHandle.nullDevice
        p.standardInput = FileHandle.nullDevice
        p.terminationHandler = { [weak self] proc in
            let status = proc.terminationStatus
            Task { @MainActor in self?.exited(status: status) }
        }
        do {
            try p.run()
        } catch {
            state = .failed(String(localized: "The server couldn't start: \(error.localizedDescription)"))
            return
        }
        process = p
        Power.shared.update(serverRunning: true, keepAwake: settings.keepAwake)
        watchHealth()
    }

    private func watchHealth() {
        healthTimer?.invalidate()
        let started = Date()
        healthTimer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] timer in
            Task { @MainActor in
                guard let self, self.state == .starting else { timer.invalidate(); return }
                if await Self.healthy(port: self.port) {
                    self.state = .running
                    timer.invalidate()
                } else if Date().timeIntervalSince(started) > 120 {
                    // Still not answering: a first start with a big library
                    // can take a while, so keep it running but say so.
                    self.state = .failed(String(localized: "The server started but isn't answering on port \(String(self.port)). Check the log."))
                    timer.invalidate()
                }
            }
        }
    }

    private func exited(status: Int32) {
        healthTimer?.invalidate()
        process = nil
        try? log?.close()
        log = nil
        Power.shared.update(serverRunning: false, keepAwake: settings.keepAwake)

        if stopRequested {
            state = .stopped
            let waiters = stopWaiters
            stopWaiters = []
            waiters.forEach { $0() }
            if restartAfterStop {
                restartAfterStop = false
                start()
            }
            return
        }

        // It stopped on its own. Start it again, unless it keeps doing that.
        let now = Date()
        crashTimes = crashTimes.filter { now.timeIntervalSince($0) < 120 } + [now]
        let reason = LogFile.tail(layout.logFile, lines: 1).first ?? ""
        if crashTimes.count <= 3 {
            state = .starting
            DispatchQueue.main.asyncAfter(deadline: .now() + 3) { [weak self] in
                guard let self, self.state == .starting, self.process == nil else { return }
                self.state = .stopped
                self.start()
            }
        } else {
            state = .failed(reason.isEmpty
                ? String(localized: "The server stopped (exit status \(status)). Check the log.")
                : reason)
        }
    }

    /// Shows a state without a server, for the guide's pictures.
    func preview(_ state: State) {
        self.state = state
    }

    nonisolated static func healthy(port: Int) async -> Bool {
        var request = URLRequest(url: URL(string: "http://127.0.0.1:\(port)/healthz")!)
        request.timeoutInterval = 1
        guard let (_, response) = try? await URLSession.shared.data(for: request) else { return false }
        return (response as? HTTPURLResponse)?.statusCode == 200
    }
}
