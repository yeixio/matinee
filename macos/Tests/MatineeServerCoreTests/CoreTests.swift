import XCTest
@testable import MatineeServerCore

final class CoreTests: XCTestCase {
    func testEnvFile() {
        let env = EnvFile.parse("""
        # comment
        MATINEE_TMDB_KEY=abc

        export MATINEE_SCAN_INTERVAL = 5m
        MATINEE_SERVER_NAME="Living room"
        MATINEE_PUBLIC_URL='https://x.example.com'
        not a setting
        BAD KEY=1
        #MATINEE_ADDR=:1
        """)
        XCTAssertEqual(env, [
            "MATINEE_TMDB_KEY": "abc",
            "MATINEE_SCAN_INTERVAL": "5m",
            "MATINEE_SERVER_NAME": "Living room",
            "MATINEE_PUBLIC_URL": "https://x.example.com",
        ])
        XCTAssertEqual(EnvFile.parse(EnvFile.template), [:])
    }

    func testEnvironment() {
        let layout = ServerLayout.standard(home: URL(fileURLWithPath: "/Users/sam"), executables: URL(fileURLWithPath: "/Applications/Matinee Server.app/Contents/MacOS"))
        var env = ServerEnvironment.build(base: ["HOME": "/Users/sam"], layout: layout, port: 7272, serverName: " Den ", envFile: [:])
        XCTAssertEqual(env["MATINEE_DATA_DIR"], "/Users/sam/Library/Application Support/Matinee Server")
        XCTAssertEqual(env["MATINEE_FFMPEG"], "/Applications/Matinee Server.app/Contents/MacOS/ffmpeg")
        XCTAssertEqual(env["MATINEE_ADDR"], ":7272")
        XCTAssertEqual(env["MATINEE_SERVER_NAME"], "Den")
        XCTAssertEqual(env["HOME"], "/Users/sam")
        XCTAssertEqual(ServerEnvironment.port(of: env, default: 7171), 7272)

        env = ServerEnvironment.build(base: [:], layout: layout, port: 7171, serverName: "", envFile: ["MATINEE_ADDR": "0.0.0.0:8080"])
        XCTAssertNil(env["MATINEE_SERVER_NAME"])
        XCTAssertEqual(ServerEnvironment.port(of: env, default: 7171), 8080)
        XCTAssertEqual(ServerEnvironment.port(of: ["MATINEE_ADDR": "nonsense"], default: 7171), 7171)
    }

    func testShareURL() {
        XCTAssertEqual(ShareURL.normalize("nas.local/Movies")?.absoluteString, "smb://nas.local/Movies")
        XCTAssertEqual(ShareURL.normalize(" smb://nas/Media ")?.absoluteString, "smb://nas/Media")
        XCTAssertEqual(ShareURL.normalize("\\\\nas\\TV")?.absoluteString, "smb://nas/TV")
        XCTAssertEqual(ShareURL.normalize("smb://nas/My Films")?.host, "nas")
        XCTAssertNil(ShareURL.normalize("nas.local"))
        XCTAssertNil(ShareURL.normalize("https://nas/Movies"))
        XCTAssertNil(ShareURL.normalize(""))
    }

    func testLogRotateAndTail() throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let log = dir.appendingPathComponent("matinee.log")
        try "one\ntwo\nthree\n".write(to: log, atomically: true, encoding: .utf8)
        XCTAssertEqual(LogFile.tail(log, lines: 2), ["two", "three"])
        LogFile.rotate(log, maxBytes: 100)
        XCTAssertTrue(FileManager.default.fileExists(atPath: log.path))
        LogFile.rotate(log, maxBytes: 4)
        XCTAssertFalse(FileManager.default.fileExists(atPath: log.path))
        XCTAssertTrue(FileManager.default.fileExists(atPath: log.appendingPathExtension("1").path))
    }
}
