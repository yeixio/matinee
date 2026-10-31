import AppKit
import SwiftUI

/// Draws the menu panel and the windows into PNGs for the guide
/// (`Matinee Server --snapshots <folder>`), in light and dark.
@MainActor
enum Snapshots {
    static func write(to folder: URL, app: AppDelegate) {
        try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        app.server.preview(.running)
        for (suffix, appearance) in [("light", NSAppearance.Name.aqua), ("dark", .darkAqua)] {
            NSApp.appearance = NSAppearance(named: appearance)
            save(MenuView(server: app.server, updates: app.updates, actions: MenuActions())
                    .background(.regularMaterial),
                 to: folder.appendingPathComponent("mac-menu-\(suffix).png"))
            save(WelcomeView(server: app.server, settings: app.settings, close: {}),
                 to: folder.appendingPathComponent("mac-welcome-\(suffix).png"))
            save(SettingsView(server: app.server, settings: app.settings, updates: app.updates),
                 to: folder.appendingPathComponent("mac-settings-\(suffix).png"))
        }
    }

    private static func save<V: View>(_ view: V, to url: URL) {
        let host = NSHostingView(rootView: view.background(Color(nsColor: .windowBackgroundColor)))
        let size = host.fittingSize
        host.frame = NSRect(origin: .zero, size: size)
        let window = NSWindow(contentRect: host.frame, styleMask: [.borderless], backing: .buffered, defer: false)
        window.contentView = host
        window.layoutIfNeeded()
        host.layoutSubtreeIfNeeded()
        host.display()
        // Twice the size, as on a Retina screen.
        guard let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: Int(size.width) * 2, pixelsHigh: Int(size.height) * 2,
                                         bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                                         colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0) else { return }
        rep.size = size
        host.cacheDisplay(in: host.bounds, to: rep)
        try? rep.representation(using: .png, properties: [:])?.write(to: url)
        print("wrote \(url.path) (\(Int(size.width))×\(Int(size.height)))")
    }
}
