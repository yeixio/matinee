import Foundation

/// Where Matinee Server keeps things, and the environment the server runs in.
public struct ServerLayout {
    public var dataDir: URL
    public var logFile: URL
    public var envFile: URL
    public var ffmpeg: URL
    public var ffprobe: URL

    public init(dataDir: URL, logFile: URL, envFile: URL, ffmpeg: URL, ffprobe: URL) {
        self.dataDir = dataDir
        self.logFile = logFile
        self.envFile = envFile
        self.ffmpeg = ffmpeg
        self.ffprobe = ffprobe
    }

    /// ~/Library/Application Support/Matinee Server and
    /// ~/Library/Logs/Matinee Server, with FFmpeg from the app bundle.
    public static func standard(home: URL, executables: URL) -> ServerLayout {
        let support = home.appendingPathComponent("Library/Application Support/Matinee Server", isDirectory: true)
        return ServerLayout(
            dataDir: support,
            logFile: home.appendingPathComponent("Library/Logs/Matinee Server/matinee.log"),
            envFile: support.appendingPathComponent("matinee.env"),
            ffmpeg: executables.appendingPathComponent("ffmpeg"),
            ffprobe: executables.appendingPathComponent("ffprobe")
        )
    }
}

public enum ServerEnvironment {
    /// The server's environment: the app's own, then the app's choices, then
    /// the advanced settings file, which wins.
    public static func build(base: [String: String], layout: ServerLayout, port: Int, serverName: String, envFile: [String: String]) -> [String: String] {
        var env = base
        env["MATINEE_DATA_DIR"] = layout.dataDir.path
        env["MATINEE_FFMPEG"] = layout.ffmpeg.path
        env["MATINEE_FFPROBE"] = layout.ffprobe.path
        env["MATINEE_ADDR"] = ":\(port)"
        // The library picker starts at the top, so /Volumes (network shares
        // and drives) and the home folder are both a click away.
        env["MATINEE_MEDIA_ROOT"] = "/"
        let name = serverName.trimmingCharacters(in: .whitespaces)
        if !name.isEmpty { env["MATINEE_SERVER_NAME"] = name }
        for (key, value) in envFile { env[key] = value }
        return env
    }

    /// The port the server will listen on, from MATINEE_ADDR (":7171",
    /// "0.0.0.0:7171") or the default.
    public static func port(of env: [String: String], default fallback: Int) -> Int {
        guard let addr = env["MATINEE_ADDR"], let colon = addr.lastIndex(of: ":"),
              let port = Int(addr[addr.index(after: colon)...]), (1...65535).contains(port) else { return fallback }
        return port
    }
}

public enum LogFile {
    /// Moves a log over maxBytes to <name>.1 (replacing the old one), so the
    /// log never grows without end.
    public static func rotate(_ url: URL, maxBytes: Int, fileManager: FileManager = .default) {
        guard let size = (try? fileManager.attributesOfItem(atPath: url.path))?[.size] as? Int, size > maxBytes else { return }
        let old = url.appendingPathExtension("1")
        try? fileManager.removeItem(at: old)
        try? fileManager.moveItem(at: url, to: old)
    }

    /// The last lines of a log, for saying why the server stopped.
    public static func tail(_ url: URL, lines: Int, maxBytes: Int = 16384) -> [String] {
        guard let handle = try? FileHandle(forReadingFrom: url) else { return [] }
        defer { try? handle.close() }
        let end = (try? handle.seekToEnd()) ?? 0
        try? handle.seek(toOffset: end > UInt64(maxBytes) ? end - UInt64(maxBytes) : 0)
        let data = (try? handle.readToEnd()) ?? Data()
        let text = String(decoding: data, as: UTF8.self)
        return Array(text.split(whereSeparator: \.isNewline).suffix(lines).map(String.init))
    }
}

/// Network shares Matinee Server reconnects before starting the server.
public enum ShareURL {
    /// Turns what someone typed ("nas.local/Movies", "smb://nas/Movies",
    /// "\\\\nas\\Movies") into an smb:// URL, or nil when it isn't one.
    public static func normalize(_ input: String) -> URL? {
        var text = input.trimmingCharacters(in: .whitespacesAndNewlines)
        if text.isEmpty { return nil }
        if text.hasPrefix("\\\\") {
            text = "smb://" + text.dropFirst(2).replacingOccurrences(of: "\\", with: "/")
        }
        if !text.contains("://") { text = "smb://" + text }
        guard let url = URL(string: text) ?? URL(string: text.addingPercentEncoding(withAllowedCharacters: .urlFragmentAllowed) ?? ""),
              let scheme = url.scheme?.lowercased(), ["smb", "afp", "nfs"].contains(scheme),
              let host = url.host, !host.isEmpty, url.path.count > 1 else { return nil }
        return url
    }
}
