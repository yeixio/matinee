import Foundation

/// The advanced settings file, matinee.env: one MATINEE_SETTING=value per
/// line, the same settings the server reads everywhere else.
public enum EnvFile {
    /// Reads KEY=value lines. Blank lines and lines starting with # are
    /// skipped, an `export ` prefix is allowed, and a value in matching single
    /// or double quotes loses its quotes.
    public static func parse(_ text: String) -> [String: String] {
        var values: [String: String] = [:]
        for raw in text.split(whereSeparator: \.isNewline) {
            var line = raw.trimmingCharacters(in: .whitespaces)
            if line.isEmpty || line.hasPrefix("#") { continue }
            if line.hasPrefix("export ") { line = String(line.dropFirst(7)).trimmingCharacters(in: .whitespaces) }
            guard let eq = line.firstIndex(of: "=") else { continue }
            let key = line[..<eq].trimmingCharacters(in: .whitespaces)
            guard !key.isEmpty, key.allSatisfy({ $0 == "_" || $0.isLetter || $0.isNumber }) else { continue }
            var value = line[line.index(after: eq)...].trimmingCharacters(in: .whitespaces)
            if value.count >= 2, let first = value.first, first == "\"" || first == "'", value.last == first {
                value = String(value.dropFirst().dropLast())
            }
            values[key] = value
        }
        return values
    }

    /// What a new matinee.env holds: every line commented out.
    public static let template = """
    # Advanced settings for the Matinee server, one per line, such as
    #   MATINEE_TMDB_KEY=abc123
    # Restart the server from the menu bar after a change. Every setting is
    # listed at https://matinee.yeix.io/docs/server/configuration/
    #
    # The port and the server's name are set in Settings in the menu, and
    # this file wins over them.

    #MATINEE_SCAN_INTERVAL=15m

    """
}
