import AppKit
import Foundation
import IOKit.pwr_mgt
import NetFS
import ServiceManagement
import SwiftUI

/// The app's own settings. The server's settings live in the server.
@MainActor
final class AppSettings: ObservableObject {
    private let defaults = UserDefaults.standard

    @Published var port: Int { didSet { defaults.set(port, forKey: "port") } }
    @Published var serverName: String { didSet { defaults.set(serverName, forKey: "serverName") } }
    @Published var keepAwake: Bool { didSet { defaults.set(keepAwake, forKey: "keepAwake") } }
    @Published var shares: [String] { didSet { defaults.set(shares, forKey: "shares") } }
    @Published var welcomed: Bool { didSet { defaults.set(welcomed, forKey: "welcomed") } }

    init() {
        defaults.register(defaults: ["port": 7171, "keepAwake": true])
        port = defaults.integer(forKey: "port")
        serverName = defaults.string(forKey: "serverName") ?? ""
        keepAwake = defaults.bool(forKey: "keepAwake")
        shares = defaults.stringArray(forKey: "shares") ?? []
        welcomed = defaults.bool(forKey: "welcomed")
    }

    /// Open at login, through the system's login items.
    var openAtLogin: Bool {
        get { SMAppService.mainApp.status == .enabled }
        set {
            objectWillChange.send()
            do {
                if newValue { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
            } catch {
                NSLog("Matinee Server: login item: \(error)")
            }
        }
    }
}

/// Keeps the Mac from going to sleep on its own while the server runs, so
/// the TV doesn't lose it mid-film. The display can still sleep.
@MainActor
final class Power {
    static let shared = Power()
    private var assertion: IOPMAssertionID = 0

    func update(serverRunning: Bool, keepAwake: Bool) {
        let want = serverRunning && keepAwake
        if want, assertion == 0 {
            IOPMAssertionCreateWithName(kIOPMAssertPreventUserIdleSystemSleep as CFString,
                                        IOPMAssertionLevel(kIOPMAssertionLevelOn),
                                        "Matinee is serving your library" as CFString, &assertion)
        } else if !want, assertion != 0 {
            IOPMAssertionRelease(assertion)
            assertion = 0
        }
    }
}

/// Network shares (smb://nas/Movies) that are connected before the server
/// starts, so libraries on them are there after a restart.
enum Shares {
    /// Connects each share that isn't connected yet. Saved passwords come from
    /// the keychain; the system asks for one it doesn't have.
    static func mountAll(_ shares: [String]) async {
        for share in shares {
            guard let url = URL(string: share) else { continue }
            _ = await mount(url)
        }
    }

    /// Connects one share and returns where it's mounted (/Volumes/Movies),
    /// or an error.
    static func mount(_ url: URL) async -> Result<URL?, Error> {
        await withCheckedContinuation { continuation in
            let openOptions = NSMutableDictionary()
            openOptions["UIOption"] = "AllowUI"
            var request: AsyncRequestID?
            let status = NetFSMountURLAsync(url as CFURL, nil, nil, nil, openOptions, nil, &request, DispatchQueue.main) { status, _, mountpoints in
                // EEXIST: it's already connected.
                if status == 0 || status == EEXIST {
                    let point = (mountpoints as? [String])?.first.map { URL(fileURLWithPath: $0) }
                    continuation.resume(returning: .success(point))
                } else {
                    continuation.resume(returning: .failure(NSError(domain: NSPOSIXErrorDomain, code: Int(status))))
                }
            }
            if status != 0 {
                continuation.resume(returning: .failure(NSError(domain: NSPOSIXErrorDomain, code: Int(status))))
            }
        }
    }
}

enum NetworkAddress {
    /// This Mac's address on the home network (Wi-Fi or Ethernet), such as
    /// 192.168.1.20.
    static func lanIPv4() -> String? {
        var first: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&first) == 0, let first else { return nil }
        defer { freeifaddrs(first) }
        var found: String?
        for ptr in sequence(first: first, next: { $0.pointee.ifa_next }) {
            let ifa = ptr.pointee
            let name = String(cString: ifa.ifa_name)
            guard name.hasPrefix("en"), let addr = ifa.ifa_addr, addr.pointee.sa_family == UInt8(AF_INET),
                  (Int32(ifa.ifa_flags) & IFF_UP) != 0 else { continue }
            var host = [CChar](repeating: 0, count: Int(NI_MAXHOST))
            if getnameinfo(addr, socklen_t(addr.pointee.sa_len), &host, socklen_t(host.count), nil, 0, NI_NUMERICHOST) == 0 {
                let ip = String(cString: host)
                if ip.hasPrefix("169.254.") { continue }
                // en0 first: it's the built-in port.
                if name == "en0" { return ip }
                found = found ?? ip
            }
        }
        return found
    }
}
